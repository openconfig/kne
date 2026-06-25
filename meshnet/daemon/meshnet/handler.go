package meshnet

import (
	"context"
	"os"

	"github.com/networkop/meshnet-cni/api/types/v1beta1"
	"github.com/networkop/meshnet-cni/daemon/grpcwire"
	"github.com/networkop/meshnet-cni/daemon/vxlan"

	log "github.com/sirupsen/logrus"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/util/retry"

	mpb "github.com/networkop/meshnet-cni/daemon/proto/meshnet/v1beta1"
)

func (m *Meshnet) getPod(ctx context.Context, name, ns string) (*unstructured.Unstructured, error) {
	mnetdLogger.Infof("Reading pod %s from K8s", name)
	return m.tClient.Topology(ns).Unstructured(ctx, name, metav1.GetOptions{})
}

func (m *Meshnet) updateStatus(ctx context.Context, obj *unstructured.Unstructured, ns string) error {
	mnetdLogger.Infof("Update pod status %s from K8s", obj.GetName())
	_, err := m.tClient.Topology(ns).Update(ctx, obj, metav1.UpdateOptions{})
	return err
}

func (m *Meshnet) Get(ctx context.Context, pod *mpb.PodQuery) (*mpb.Pod, error) {
	mnetdLogger.Infof("Retrieving %s's metadata from K8s...", pod.Name)

	result, err := m.getPod(ctx, pod.Name, pod.KubeNs)
	if err != nil {
		mnetdLogger.Errorf("Failed to read pod %s from K8s", pod.Name)
		return nil, err
	}

	remoteLinks, found, err := unstructured.NestedSlice(result.Object, "spec", "links")
	if err != nil || !found || remoteLinks == nil {
		mnetdLogger.Errorf("could not find 'Link' array in pod's spec")
		return nil, err
	}

	links := make([]*mpb.Link, len(remoteLinks))
	for i := range links {
		remoteLink, ok := remoteLinks[i].(map[string]interface{})
		if !ok {
			mnetdLogger.Errorf("Unrecognised 'Link' structure")
			return nil, err
		}
		newLink := &mpb.Link{}
		newLink.PeerPod, _, _ = unstructured.NestedString(remoteLink, "peer_pod")
		newLink.PeerIntf, _, _ = unstructured.NestedString(remoteLink, "peer_intf")
		newLink.LocalIntf, _, _ = unstructured.NestedString(remoteLink, "local_intf")
		newLink.LocalIp, _, _ = unstructured.NestedString(remoteLink, "local_ip")
		newLink.PeerIp, _, _ = unstructured.NestedString(remoteLink, "peer_ip")
		newLink.Uid, _, _ = unstructured.NestedInt64(remoteLink, "uid")
		links[i] = newLink
	}

	srcIP, _, _ := unstructured.NestedString(result.Object, "status", "src_ip")
	netNs, _, _ := unstructured.NestedString(result.Object, "status", "net_ns")
	containerId, _, _ := unstructured.NestedString(result.Object, "status", "container_id")
	nodeIP := os.Getenv("HOST_IP")
	nodeIntf := os.Getenv("HOST_INTF")

	return &mpb.Pod{
		Name:        pod.Name,
		SrcIp:       srcIP,
		NetNs:       netNs,
		KubeNs:      pod.KubeNs,
		Links:       links,
		NodeIp:      nodeIP,
		NodeIntf:    nodeIntf,
		ContainerId: containerId,
	}, nil
}

func (m *Meshnet) SetAlive(ctx context.Context, pod *mpb.Pod) (*mpb.BoolResponse, error) {
	mnetdLogger.Infof("Setting %s's SrcIp=%s and NetNs=%s", pod.Name, pod.SrcIp, pod.NetNs)

	retryErr := retry.RetryOnConflict(retry.DefaultRetry, func() error {
		result, err := m.getPod(ctx, pod.Name, pod.KubeNs)
		if err != nil {
			mnetdLogger.Errorf("Failed to read pod %s from K8s", pod.Name)
			return err
		}

		if err = unstructured.SetNestedField(result.Object, pod.SrcIp, "status", "src_ip"); err != nil {
			mnetdLogger.Errorf("Failed to update pod's src_ip")
		}

		if err = unstructured.SetNestedField(result.Object, pod.NetNs, "status", "net_ns"); err != nil {
			mnetdLogger.Errorf("Failed to update pod's net_ns")
		}

		if err = unstructured.SetNestedField(result.Object, pod.ContainerId, "status", "container_id"); err != nil {
			mnetdLogger.Errorf("Failed to update pod's container_id")
		}

		return m.updateStatus(ctx, result, pod.KubeNs)
	})

	if retryErr != nil {
		log.WithFields(log.Fields{
			"daemon":   "meshnetd",
			"err":      retryErr,
			"function": "SetAlive",
		}).Errorf("Failed to update pod %s alive status", pod.Name)
		return &mpb.BoolResponse{Response: false}, retryErr
	}

	return &mpb.BoolResponse{Response: true}, nil
}

// A point to point link between two pods is created only when both the pods are alive.
// While creating a link, when a pod does not find its peer, it marks the peer as skipped for this link UID.
// The pod stores the list of skipped peers and its corresponding link UID in it's skip list.
// For example - when a pod A want to create a link and it does not find it's peer B,
// then pod A adds the peer pod B and corresponding link UID in pod A's skip list.
func (m *Meshnet) Skip(ctx context.Context, skip *mpb.SkipQuery) (*mpb.BoolResponse, error) {
	mnetdLogger.Infof("Pod %s, skipping peer pod %s for link UID %d ", skip.Pod, skip.Peer, skip.LinkId)

	retryErr := retry.RetryOnConflict(retry.DefaultRetry, func() error {
		result, err := m.getPod(ctx, skip.Pod, skip.KubeNs)
		if err != nil {
			mnetdLogger.Errorf("Failed to read pod %s from K8s", skip.Pod)
			return err
		}
		skipped, found, err := unstructured.NestedSlice(result.Object, "status", "skipped")
		if found && err != nil {
			mnetdLogger.Errorf("skip: error in retrieving skipped list from status, object found: %t, err: %v", found, err)
			return err
		}
		// create a new skip item to append the peer in the skip list
		newItem, err := runtime.DefaultUnstructuredConverter.ToUnstructured(&v1beta1.Skipped{PodName: skip.Peer, LinkId: skip.LinkId})
		if err != nil {
			mnetdLogger.Errorf("skip: could not convert to unstructured: %v\n", err)
			return err
		}
		//append the peer information in skip list and update the data store
		newSkipped := append(skipped, newItem)
		if err := unstructured.SetNestedField(result.Object, newSkipped, "status", "skipped"); err != nil {
			mnetdLogger.Errorf("failed to updated skipped list")
			return err
		}

		return m.updateStatus(ctx, result, skip.KubeNs)
	})
	if retryErr != nil {
		log.WithFields(log.Fields{
			"daemon":   "meshnetd",
			"err":      retryErr,
			"function": "Skip",
		}).Errorf("Failed to update skip pod %s status", skip.Pod)
		return &mpb.BoolResponse{Response: false}, retryErr
	}

	return &mpb.BoolResponse{Response: true}, nil
}

// Clean up function SkipReverse is called when a pod is getting deleted. It gets called once for every link the pod has.
// Anytime a pod is destroyed, for a link, it inserts itself along with its link-id in the skip list of its' peers on the link.
// It removes the the peer (and connecting link UID) from its own skip list.
// This data update in "data-store" helps to recreate the links in future when the pod is recreated by K8S
func (m *Meshnet) SkipReverse(ctx context.Context, skip *mpb.SkipQuery) (*mpb.BoolResponse, error) {
	mnetdLogger.Infof("Reverse-skip for pod %s on link uid %d with peer pod %s", skip.Pod, skip.LinkId, skip.Peer)

	var podName string
	retryErr := retry.RetryOnConflict(retry.DefaultRetry, func() error {
		// get the peer pod if it is alive
		peerPod, err := m.getPod(ctx, skip.Peer, skip.KubeNs)
		if err != nil {
			mnetdLogger.Errorf("Failed to read pod %s from K8s", skip.Pod)
			return err
		}
		srcIP, _, _ := unstructured.NestedString(peerPod.Object, "status", "src_ip")
		netNs, _, _ := unstructured.NestedString(peerPod.Object, "status", "net_ns")
		if srcIP == "" || netNs == "" {
			// peer pod is not alive as container. so no need to update peer's skipped list in Topology CRD.
			return nil
		}
		podName = peerPod.GetName()

		// Extracting peer pod's skipped list and insert this pod's name in it.
		// This is needed as in future this pod comes back agin, it will find out that it has
		// been skipped by the peer. As a result this pod will re-initiate link creation.
		peerSkipped, found, err := unstructured.NestedSlice(peerPod.Object, "status", "skipped")
		if found && err != nil {
			mnetdLogger.Errorf("skipReverse: error in retrieving skipped list from peer pod's status: object found: %t, err: %v", found, err)
			return err
		}

		// If the pod is already present in skipped list we don't need to append it again.
		// For example - The pod can be already present in the Peer's skip list if this was a low priority
		// pod and it came up after the high priority peer.
		// For example :- when a pod is repeatedly destroyed (create->destroy->create->destroy.... while
		// the topology is still alive), it try to insert itself multiple times in peers skip list.
		// Prevent multiple entry of same pod.
		found = false
		for _, el := range peerSkipped {
			elSkipped, ok := el.(map[string]interface{})
			if !ok {
				mnetdLogger.Errorf("skipReverse: 'Skipped' not recognized")
				continue
			}
			skipped := v1beta1.Skipped{}
			if err := runtime.DefaultUnstructuredConverter.FromUnstructured(elSkipped, &skipped); err != nil {
				mnetdLogger.Errorf("skipReverse: unable to retrieve Skipped: %v", err)
				continue
			}
			if skip.Pod == skipped.PodName && skip.LinkId == skipped.LinkId {
				found = true
				break
			}
		}
		newPeerSkipped := peerSkipped
		if !found {
			newItem, err := runtime.DefaultUnstructuredConverter.ToUnstructured(&v1beta1.Skipped{PodName: skip.Pod, LinkId: skip.LinkId})
			if err != nil {
				mnetdLogger.Errorf("skipReverse: could not convert to unstructured: %v\n", err)
				return err
			}
			newPeerSkipped = append(newPeerSkipped, newItem)
		}

		// updating peer pod's skipped list locally
		if err := unstructured.SetNestedField(peerPod.Object, newPeerSkipped, "status", "skipped"); err != nil {
			mnetdLogger.Errorf("skipReverse: Failed to updated reverse-skipped list for peer pod %s", peerPod.GetName())
			return err
		}

		// sending peer pod's updates to k8s
		return m.updateStatus(ctx, peerPod, skip.KubeNs)
	})
	if retryErr != nil {
		log.WithFields(log.Fields{
			"daemon":   "meshnetd",
			"err":      retryErr,
			"function": "SkipReverse",
		}).Errorf("Failed to update peer pod %s skipreverse status", podName)
		return &mpb.BoolResponse{Response: false}, retryErr
	}

	retryErr = retry.RetryOnConflict(retry.DefaultRetry, func() error {
		// setting the value for this pod
		thisPod, err := m.getPod(ctx, skip.Pod, skip.KubeNs)
		if err != nil {
			mnetdLogger.Errorf("skipReverse: Failed to read pod %s from K8s", skip.Pod)
			return err
		}

		// extracting this pod's skipped list and removing peer pod's name from it
		thisSkipped, found, err := unstructured.NestedSlice(thisPod.Object, "status", "skipped")
		if found && err != nil {
			mnetdLogger.Errorf("skipreverse: error in retrieving skipped list from local pod's status: object found: %t, err: %v", found, err)
			return err
		}
		newThisSkipped := make([]interface{}, 0)

		log.WithFields(log.Fields{
			"daemon":      "meshnetd",
			"SkipReverse": thisSkipped,
		}).Info("THIS SkipReverse:")

		for _, el := range thisSkipped {
			elSkipped, ok := el.(map[string]interface{})
			if !ok {
				mnetdLogger.Errorf("skip-reverse: unrecongnized 'Skipped'")
				continue
			}
			skipped := v1beta1.Skipped{}
			if err := runtime.DefaultUnstructuredConverter.FromUnstructured(elSkipped, &skipped); err != nil {
				mnetdLogger.Errorf("skipReverse : unable to retrieve Skipped: %v", err)
				continue
			}
			if skipped.PodName != skip.Peer && skipped.LinkId != skip.LinkId {
				mnetdLogger.Infof("Appending new element %s@%d", skipped.PodName, skipped.LinkId)
				newThisSkipped = append(newThisSkipped, elSkipped)
			}
		}

		log.WithFields(log.Fields{
			"daemon":      "meshnetd",
			"SkipReverse": newThisSkipped,
		}).Info("NEW THIS SkipReverse:")

		// updating this pod's skipped list locally after removing the peer from the current skip list.
		// Length check for newThisSkipped is not needed even if it is empty. we should update data-store with empty list.
		if err := unstructured.SetNestedField(thisPod.Object, newThisSkipped, "status", "skipped"); err != nil {
			mnetdLogger.Errorf("skipReverse: Failed to cleanup skipped list for pod %s", thisPod.GetName())
			return err
		}

		// sending this pod's updates to k8s
		return m.updateStatus(ctx, thisPod, skip.KubeNs)
	})
	if retryErr != nil {
		log.WithFields(log.Fields{
			"daemon":   "meshnetd",
			"err":      retryErr,
			"function": "SkipReverse",
		}).Error("Failed to update this pod skipreverse status")
		return &mpb.BoolResponse{Response: false}, retryErr
	}

	return &mpb.BoolResponse{Response: true}, nil
}

func (m *Meshnet) IsSkipped(ctx context.Context, skip *mpb.SkipQuery) (*mpb.BoolResponse, error) {
	// mnetdLogger.Infof("Checking if %s is skipped by %s", skip.Peer, skip.Pod)
	mnetdLogger.Infof("Checking if %s, link id %d is skipped by %s", skip.Pod, skip.LinkId, skip.Peer)

	result, err := m.getPod(ctx, skip.Peer, skip.KubeNs)
	if err != nil {
		mnetdLogger.Errorf("isSkipped: Failed to read pod %s from K8s", skip.Pod)
		return nil, err
	}

	skipped, found, err := unstructured.NestedSlice(result.Object, "status", "skipped")
	if found && err != nil {
		mnetdLogger.Errorf("isSkipped: error in retrieving skipped list from peer pod's status, object found: %t, err: %v", found, err)
		return nil, err
	}

	for _, peerSkipped := range skipped {
		elSkipped, ok := peerSkipped.(map[string]interface{})
		if !ok {
			mnetdLogger.Errorf("isSkipped: 'Skipped' not recognized")
			continue
		}
		skipped := v1beta1.Skipped{}
		if err := runtime.DefaultUnstructuredConverter.FromUnstructured(elSkipped, &skipped); err != nil {
			mnetdLogger.Errorf("isSkipped: unable to retrieve Skipped: %v", err)
			continue
		}
		if skip.Pod == skipped.PodName && skip.LinkId == skipped.LinkId {
			return &mpb.BoolResponse{Response: true}, nil
		}
	}
	return &mpb.BoolResponse{Response: false}, nil
}

func (m *Meshnet) Update(ctx context.Context, pod *mpb.RemotePod) (*mpb.BoolResponse, error) {
	if err := vxlan.CreateOrUpdate(pod); err != nil {
		mnetdLogger.Errorf("Failed to Update Vxlan")
		return &mpb.BoolResponse{Response: false}, nil
	}
	return &mpb.BoolResponse{Response: true}, nil
}

// ------------------------------------------------------------------------------------------------------
func (m *Meshnet) RemGRPCWire(ctx context.Context, wireDef *mpb.WireDef) (*mpb.BoolResponse, error) {
	//if err := grpcwire.DeleteWiresByPod(wireDef.KubeNs, wireDef.LocalPodName); err != nil
	if err := grpcwire.DeletePodWires(wireDef.TopoNs, wireDef.LocalPodName); err != nil {
		return &mpb.BoolResponse{Response: false}, err
	}
	return &mpb.BoolResponse{Response: true}, nil
}

func (m *Meshnet) AddGRPCWireLocal(ctx context.Context, wireDef *mpb.WireDef) (*mpb.BoolResponse, error) {
	return grpcwire.CreateGRPCWireLocal(ctx, wireDef)
}

// ------------------------------------------------------------------------------------------------------
func (m *Meshnet) SendToOnce(ctx context.Context, pkt *mpb.Packet) (*mpb.BoolResponse, error) {
	wrHandle, err := grpcwire.GetHostIntfHndl(pkt.RemotIntfId)
	if err != nil {
		log.WithFields(log.Fields{
			"daemon":  "meshnetd",
			"overlay": "gRPC",
		}).Errorf("SendToOnce (wire id - %v): Could not find local handle. err:%v", pkt.RemotIntfId, err)
		return &mpb.BoolResponse{Response: false}, err
	}

	// In case any per packet log need to be generated.
	// pktType := grpcwire.DecodePkt(pkt.Frame)
	// log.Printf("Daemon(SendToOnce): Received [pkt: %s, bytes: %d, for local interface id: %d]. Sending it to local container", pktType, len(pkt.Frame), pkt.RemotIntfId)
	// log.Printf("Daemon(SendToOnce): Received [bytes: %d, for local interface id: %d]. Sending it to local container", len(pkt.Frame), pkt.RemotIntfId)

	err = wrHandle.WritePacketData(pkt.Frame)
	if err != nil {
		log.WithFields(log.Fields{
			"daemon":  "meshnetd",
			"overlay": "gRPC",
		}).Errorf("SendToOnce (wire id - %v): Could not write packet(%d bytes) to local interface. err:%v", pkt.RemotIntfId, len(pkt.Frame), err)
		return &mpb.BoolResponse{Response: false}, err
	}

	return &mpb.BoolResponse{Response: true}, nil
}

// ---------------------------------------------------------------------------------------------------------------
func (m *Meshnet) AddGRPCWireRemote(ctx context.Context, wireDef *mpb.WireDef) (*mpb.WireCreateResponse, error) {
	stopC := make(chan struct{})
	wire, err := grpcwire.CreateUpdateGRPCWireRemoteTriggered(wireDef, stopC)
	if err == nil {
		log.WithFields(log.Fields{
			"daemon":  "meshnetd",
			"overlay": "gRPC",
		}).Infof("[ADD-WIRE:REMOTE-END]For pod %s@%s starting the local packet receive thread", wireDef.LocalPodName, wireDef.IntfNameInPod)
		go grpcwire.RecvFrmLocalPodThread(wire, wire.LocalNodeIfaceName)

		return &mpb.WireCreateResponse{Response: true, PeerIntfId: wire.LocalNodeIfaceID}, nil
	}
	log.WithFields(log.Fields{
		"daemon":  "meshnetd",
		"overlay": "gRPC",
	}).Errorf("[ADD-WIRE:REMOTE-END] err: %v", err)
	return &mpb.WireCreateResponse{Response: false, PeerIntfId: wireDef.WireIfIdOnPeerNode}, err
}

// ---------------------------------------------------------------------------------------------------------------
func (m *Meshnet) GRPCWireDownRemote(ctx context.Context, wireDef *mpb.WireDef) (*mpb.WireDownResponse, error) {
	err := grpcwire.GRPCWireDownRemoteTriggered(wireDef)
	if err == nil {
		log.WithFields(log.Fields{
			"daemon":  "meshnetd",
			"overlay": "gRPC",
		}).Infof("[WIRE-DOWN]At remote end for pod %s@%s", wireDef.LocalPodName, wireDef.IntfNameInPod)
		return &mpb.WireDownResponse{Response: true}, nil
	}
	log.WithFields(log.Fields{
		"daemon":  "meshnetd",
		"overlay": "gRPC",
	}).Errorf("[WIRE-DOWN]Remote end err: %v", err)
	return &mpb.WireDownResponse{Response: false}, err
}

// ---------------------------------------------------------------------------------------------------------------
// GRPCWireExists will return the wire if it exists.
func (m *Meshnet) GRPCWireExists(ctx context.Context, wireDef *mpb.WireDef) (*mpb.WireCreateResponse, error) {
	wire, ok := grpcwire.GetWireByUID(wireDef.LocalPodNetNs, int(wireDef.LinkUid))
	if !ok || wire == nil {
		return &mpb.WireCreateResponse{Response: false, PeerIntfId: wireDef.WireIfIdOnPeerNode}, nil
	}
	return &mpb.WireCreateResponse{Response: ok, PeerIntfId: wire.WireIfaceIDOnPeerNode}, nil
}

// ---------------------------------------------------------------------------------------------------------------
// Given the pod name and the pod interface, GenerateNodeInterfaceName generates the corresponding interface name in the node.
// This pod interface and the node interface later become the two end of a veth-pair
func (m *Meshnet) GenerateNodeInterfaceName(ctx context.Context, in *mpb.GenerateNodeInterfaceNameRequest) (*mpb.GenerateNodeInterfaceNameResponse, error) {
	locIfNm, err := grpcwire.GenNodeIfaceName(in.PodName, in.PodIntfName)
	if err != nil {
		return &mpb.GenerateNodeInterfaceNameResponse{Ok: false, NodeIntfName: ""}, err
	}
	return &mpb.GenerateNodeInterfaceNameResponse{Ok: true, NodeIntfName: locIfNm}, nil
}
