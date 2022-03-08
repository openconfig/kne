package ixia

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	log "github.com/sirupsen/logrus"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"

	topologyv1 "github.com/google/kne/api/types/v1beta1"
	tpb "github.com/google/kne/proto/topo"
	"github.com/google/kne/topo/node"
)

type IxiaTGSvcPort struct {
	In  int32 `json:"in"`
	Out int32 `json:"out,omitempty"`
	//InIp     string `json:"inside_ip,omitempty"`
	//OutIp    string `json:"outside_ip,omitempty"`
	//NodePort int32 `json:"node_port,omitempty"`
}

type IxiaTGIntf struct {
	Name  string `json:"name"`
	Group string `json:"group,omitempty"`
}

type IxiaTGIntfStatus struct {
	PodName string `json:"pod_name"`
	Name    string `json:"name"`
}

// IxiaTGSpec defines the desired state of IxiaTG
type IxiaTGSpec struct {
	// INSERT ADDITIONAL SPEC FIELDS - desired state of cluster
	// Important: Run "make" to regenerate code after modifying this file

	// Version of the node
	Release string `json:"release,omitempty"`
	// Desired state by network emulation (KNE)
	DesiredState string `json:"desired_state,omitempty"`
	// ApiEndPoint as define in OTG config
	ApiEndPoint map[string]IxiaTGSvcPort `json:"api_endpoint_map,omitempty"`
	// Interfaces with DUT
	Interfaces []IxiaTGIntf `json:"interfaces,omitempty"`
}

// IxiaTGStatus defines the observed state of IxiaTG
type IxiaTGStatus struct {
	// INSERT ADDITIONAL STATUS FIELD - define observed state of cluster
	// Important: Run "make" to regenerate code after modifying this file
	//Pod    string `json:"pod,omitempty"`
	State      string             `json:"state,omitempty"`
	Reason     string             `json:"reason,omitempty"`
	Interfaces []IxiaTGIntfStatus `json:"interfaces,omitempty"`
}

//+kubebuilder:object:root=true
//+kubebuilder:subresource:status

// IxiaTG is the Schema for the ixiacs API
//+kubebuilder:subresource:status
type IxiaTG struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   IxiaTGSpec   `json:"spec,omitempty"`
	Status IxiaTGStatus `json:"status,omitempty"`
}

var ixiaCrd *IxiaTG
var ixiaResource = "Ixiatgs"

func New(nodeImpl *node.Impl) (node.Node, error) {
	if nodeImpl == nil {
		return nil, fmt.Errorf("nodeImpl cannot be nil")
	}
	if nodeImpl.Proto == nil {
		return nil, fmt.Errorf("nodeImpl.Proto cannot be nil")
	}
	cfg := defaults(nodeImpl.Proto)
	nodeImpl.Proto = cfg
	n := &Node{
		Impl: nodeImpl,
	}

	n.FixInterfaces()
	return n, nil
}

type Node struct {
	*node.Impl
}

func (n *Node) getCrd(new bool) *IxiaTG {
	if new {
		ixiaCrd = nil
	}

	if ixiaCrd == nil {
		log.Infof("Creating ixia CRD for node: %v", n.Name())
		ixiaCrd = &IxiaTG{
			TypeMeta: metav1.TypeMeta{
				APIVersion: "network.keysight.com/v1alpha1",
				Kind:       "IxiaTG",
			},
			ObjectMeta: metav1.ObjectMeta{
				Name:      n.Name(),
				Namespace: n.Namespace,
			},
			Spec: IxiaTGSpec{
				Release:      n.Proto.Version,
				DesiredState: "INITIATED",
				ApiEndPoint:  map[string]IxiaTGSvcPort{},
				Interfaces:   []IxiaTGIntf{},
			},
		}

		for _, svc := range n.GetProto().Services {
			ixiaCrd.Spec.ApiEndPoint[svc.Name] = IxiaTGSvcPort{
				In:  int32(svc.Inside),
				Out: int32(svc.Outside),
			}
		}
		for name := range n.GetProto().Interfaces {
			ixiaCrd.Spec.Interfaces = append(ixiaCrd.Spec.Interfaces, IxiaTGIntf{
				Name: name,
			})
		}

		log.Tracef("Created ixia CRD for node %s: %+q", n.Name(), ixiaCrd)
	}

	return ixiaCrd
}

func (n *Node) getStatus(ctx context.Context) (*IxiaTGStatus, error) {
	r := n.KubeClient.CoreV1().RESTClient().
		Get().
		AbsPath("/apis/network.keysight.com/v1alpha1").
		Namespace(n.Namespace).
		Resource(ixiaResource).
		Name(n.Name()).
		Do(ctx)

	if err := r.Error(); err != nil {
		return nil, fmt.Errorf("could not get ixia CRD: %v", err)
	}

	rBytes, err := r.Raw()
	if err != nil {
		return nil, fmt.Errorf("could not get raw ixia CRD response: %v", err)
	}

	crd := &IxiaTG{}
	if err := json.Unmarshal(rBytes, crd); err != nil {
		return nil, fmt.Errorf("could not unmarshal ixia CRD: %v", err)
	}

	return &crd.Status, nil
}

func (n *Node) waitForState(ctx context.Context, state string, dur time.Duration) (*IxiaTGStatus, error) {
	start := time.Now()

	log.Infof("Waiting for ixia CRD state to be %s ... (timeout: %v)", state, dur)
	for time.Since(start) < dur {
		status, err := n.getStatus(ctx)

		if err != nil {
			return nil, fmt.Errorf("could not get ixia CRD: %v", err)
		}

		if status.State == "FAILED" {
			return nil, fmt.Errorf("got FAILED state for ixia CRD: %s", status.Reason)
		}

		if status.State == state {
			log.Infof("Attained ixia CRD state %s", state)
			return status, nil
		}
	}

	return nil, fmt.Errorf("timed out waiting for ixia CRD state to be %s", state)
}

func (n *Node) TopologySpecs(ctx context.Context) ([]*topologyv1.Topology, error) {
	log.Infof("Getting interfaces for ixia node resource %s ...", n.Name())
	desiredState := "INITIATED"

	crd, err := json.Marshal(n.getCrd(true))
	if err != nil {
		return nil, fmt.Errorf("could not marshal ixia CRD to JSON: %v", err)
	}

	log.Infof("Creating custom resource for ixia (desiredState=%s) ...", desiredState)
	err = n.KubeClient.CoreV1().RESTClient().
		Post().
		AbsPath("/apis/network.keysight.com/v1alpha1").
		Namespace(n.Namespace).
		Resource(ixiaResource).
		Body(crd).
		Do(ctx).
		Error()

	if err != nil {
		return nil, fmt.Errorf("could not create custom resource for ixia: %v", err)
	}

	status, err := n.waitForState(ctx, desiredState, 30*time.Second)
	if err != nil {
		return nil, fmt.Errorf("could not wait for state of custom resource for ixia: %v", err)
	}

	proto := n.GetProto()
	// this is needed since ixia node can consist of one or more pods and each
	// pod may have one or more interfaces associated with it
	podNameTopo := map[string]*topologyv1.Topology{}

	for _, ifc := range status.Interfaces {
		nodeIfc, ok := proto.Interfaces[ifc.Name]
		if !ok {
			return nil, fmt.Errorf("could not find '%s' in interface map of node %s", ifc.Name, proto.Name)
		}

		topo, ok := podNameTopo[ifc.PodName]
		if !ok {
			topo = &topologyv1.Topology{
				ObjectMeta: metav1.ObjectMeta{
					Name: ifc.PodName,
				},
				Spec: topologyv1.TopologySpec{
					Links: []topologyv1.Link{},
				},
			}
			podNameTopo[ifc.PodName] = topo
		}

		topo.Spec.Links = append(topo.Spec.Links, topologyv1.Link{
			UID:       int(nodeIfc.Uid),
			LocalIntf: ifc.Name,
			PeerIntf:  nodeIfc.PeerIntName,
			PeerPod:   nodeIfc.PeerName,
			LocalIP:   "",
			PeerIP:    "",
		})
	}

	topos := make([]*topologyv1.Topology, 0, len(podNameTopo))
	for _, topo := range podNameTopo {
		topos = append(topos, topo)
	}
	return topos, nil
}

func (n *Node) Create(ctx context.Context) error {
	log.Infof("Creating deployment for node resource %s", n.Name())
	desiredState := "DEPLOYED"

	crd := n.getCrd(false)
	crd.Spec.DesiredState = desiredState

	crdBytes, err := json.Marshal(crd)
	if err != nil {
		return fmt.Errorf("could not marshal ixia CRD to JSON: %v", err)
	}

	log.Infof("Updating ixia CRD (desiredState=%s) ...", desiredState)

	// patch := jsonpatch.Patch{}
	// bytes := json.RawMessage([]byte(`{"path": "/spec/desired_state", "value": "DEPLOYED"}`))
	// patch = append(patch, jsonpatch.Operation{
	// 	"replace": &bytes,
	// })

	// patchBytes, err := json.Marshal(patch)
	// if err != nil {
	// 	return fmt.Errorf("could not marshal patch object: %v", err)
	// }

	err = n.KubeClient.CoreV1().RESTClient().
		Patch(types.MergePatchType).
		AbsPath("/apis/network.keysight.com/v1alpha1").
		Namespace(n.Namespace).
		Resource(ixiaResource).
		Name(n.Name()).
		Body(crdBytes).
		Do(ctx).
		Error()

	if err != nil {
		return fmt.Errorf("could not update ixia CRD: %v", err)
	}

	return nil
}

func (n *Node) Status(ctx context.Context) (node.NodeStatus, error) {
	state := node.NODE_FAILED

	status, err := n.getStatus(ctx)
	if err != nil {
		return state, fmt.Errorf("could not get ixia CRD: %v", err)
	}

	switch status.State {
	case "DEPLOYED":
		state = node.NODE_RUNNING
	case "INITIATED":
		state = node.NODE_PENDING
	}

	return state, nil
}

func (n *Node) Delete(ctx context.Context) error {
	log.Infof("Deleting IxiaTG node resource %s", n.Name())
	err := n.KubeClient.CoreV1().RESTClient().
		Delete().
		AbsPath("/apis/network.keysight.com/v1alpha1").
		Namespace(n.Namespace).
		Resource(ixiaResource).
		Name(n.Name()).
		Do(ctx).
		Error()

	if err != nil {
		log.Error(err)
		return err
	}
	return nil
}

func (n *Node) FixInterfaces() {
	for _, v := range n.Proto.Interfaces {
		v.Name = v.IntName
	}
}

func defaults(pb *tpb.Node) *tpb.Node {
	return pb
}

func init() {
	node.Register(tpb.Node_IXIA_TG, New)
	node.Vendor(tpb.Vendor_KEYSIGHT, New)
}
