package ixia

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	ixiatg "github.com/open-traffic-generator/ixia-c-operator/api/v1alpha1"
	log "github.com/sirupsen/logrus"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"

	topologyv1 "github.com/google/kne/api/types/v1beta1"
	tpb "github.com/google/kne/proto/topo"
	"github.com/google/kne/topo/node"
)

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

func (n *Node) newCRD() *ixiatg.IxiaTG {
	log.Infof("Creating new ixia CRD for node: %v", n.Name())
	ixiaCRD := &ixiatg.IxiaTG{
		TypeMeta: metav1.TypeMeta{
			APIVersion: "network.keysight.com/v1alpha1",
			Kind:       "IxiaTG",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      n.Name(),
			Namespace: n.Namespace,
		},
		Spec: ixiatg.IxiaTGSpec{
			Release:      n.Proto.Version,
			DesiredState: "INITIATED",
			ApiEndPoint:  map[string]ixiatg.IxiaTGSvcPort{},
			Interfaces:   []ixiatg.IxiaTGIntf{},
		},
	}

	for _, svc := range n.GetProto().Services {
		ixiaCRD.Spec.ApiEndPoint[svc.Name] = ixiatg.IxiaTGSvcPort{
			In:  int32(svc.Inside),
			Out: int32(svc.Outside),
		}
	}
	for name, ifc := range n.GetProto().Interfaces {
		ixiaCRD.Spec.Interfaces = append(ixiaCRD.Spec.Interfaces, ixiatg.IxiaTGIntf{
			Name:  name,
			Group: ifc.Group,
		})
	}

	log.Tracef("Created new ixia CRD for node %s: %+q", n.Name(), ixiaCRD)
	return ixiaCRD
}

func (n *Node) getCRD(ctx context.Context) (*ixiatg.IxiaTG, error) {
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

	crd := &ixiatg.IxiaTG{}
	if err := json.Unmarshal(rBytes, crd); err != nil {
		return nil, fmt.Errorf("could not unmarshal ixia CRD: %v", err)
	}

	return crd, nil
}

func (n *Node) getStatus(ctx context.Context) (*ixiatg.IxiaTGStatus, error) {
	crd, err := n.getCRD(ctx)
	if err != nil {
		return nil, err
	}
	return &crd.Status, nil
}

func (n *Node) waitForState(ctx context.Context, state string, dur time.Duration) (*ixiatg.IxiaTGStatus, error) {
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

		time.Sleep(50 * time.Millisecond)
	}

	return nil, fmt.Errorf("timed out waiting for ixia CRD state to be %s", state)
}

func (n *Node) TopologySpecs(ctx context.Context) ([]*topologyv1.Topology, error) {
	log.Infof("Getting interfaces for ixia node resource %s ...", n.Name())
	desiredState := "INITIATED"

	crd, err := json.Marshal(n.newCRD())
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

	crd, err := n.getCRD(ctx)
	if err != nil {
		return err
	}
	crd.Spec.DesiredState = desiredState
	crd.Status = ixiatg.IxiaTGStatus{}

	crdBytes, err := json.Marshal(crd)
	if err != nil {
		return fmt.Errorf("could not marshal ixia CRD to JSON: %v", err)
	}

	log.Infof("Updating ixia CRD (desiredState=%s) ...", desiredState)

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

// Pods returns the pod definitions for the node.
func (n *Node) Pods(ctx context.Context) ([]*corev1.Pod, error) {
	crd, err := n.getCRD(ctx)
	if err != nil {
		return nil, err
	}

	pod, err := n.KubeClient.CoreV1().Pods(n.Namespace).List(ctx, metav1.ListOptions{})
	if err != nil {
		return nil, err
	}

	pods := make([]*corev1.Pod, len(crd.Status.Interfaces)+1)
	for i := range crd.Status.Interfaces {
		for j := range pod.Items {
			if pod.Items[j].Name == crd.Status.Interfaces[i].PodName {
				pods[i+1] = &pod.Items[j]
				break
			}
		}
	}

	for i := range pod.Items {
		if pod.Items[i].Name == crd.Status.ApiEndPoint.PodName {
			pods[0] = &pod.Items[i]
			break
		}
	}

	return pods, nil
}

// Services returns the service definition for the node.
func (n *Node) Services(ctx context.Context) ([]*corev1.Service, error) {
	crd, err := n.getCRD(ctx)
	if err != nil {
		return nil, err
	}

	svc, err := n.KubeClient.CoreV1().Services(n.Namespace).List(ctx, metav1.ListOptions{})
	if err != nil {
		return nil, err
	}

	svcs := make([]*corev1.Service, len(crd.Spec.ApiEndPoint))
	for i := range crd.Status.ApiEndPoint.ServiceName {
		for j := range svc.Items {
			if svc.Items[j].Name == crd.Status.ApiEndPoint.ServiceName[i] {
				svcs[i] = &svc.Items[j]
				break
			}
		}
	}

	return svcs, nil
}

func (n *Node) Status(ctx context.Context) (node.NodeStatus, error) {
	state := node.NODE_FAILED
	var err error

	status, err := n.getStatus(ctx)
	if err != nil {
		return state, fmt.Errorf("could not get ixia CRD: %v", err)
	}

	switch status.State {
	case "DEPLOYED":
		state = node.NODE_RUNNING
	case "INITIATED":
		state = node.NODE_PENDING
	case "FAILED":
		err = fmt.Errorf("got failure in ixia CRD status: %s", status.Reason)
	}

	return state, err
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
