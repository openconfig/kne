package grpcwire

import (
	"context"

	log "github.com/sirupsen/logrus"

	mpb "github.com/openconfig/kne/third_party/meshnet/daemon/proto/meshnet/v1beta1"
	"github.com/openconfig/kne/third_party/meshnet/utils/wireutil"
)

func CreateGRPCWireLocal(ctx context.Context, wireDef *mpb.WireDef) (*mpb.BoolResponse, error) {
	tapFile, err := wireutil.CreateOrAttachTAP(wireDef.LocalPodNetNs, wireDef.IntfNameInPod, wireDef.LocalPodIp)
	if err != nil {
		log.WithFields(log.Fields{
			"daemon":  "meshnetd",
			"overlay": "gRPC",
		}).Errorf("[ADD-WIRE:LOCAL-END] For pod %s failed to create/attach TAP interface %s in netns %s: %v",
			wireDef.LocalPodName, wireDef.IntfNameInPod, wireDef.LocalPodNetNs, err)
		return &mpb.BoolResponse{Response: false}, err
	}

	wireID := NextIndex()
	aWire := CreateGWire(int(wireID), wireDef.IntfNameInPod, make(chan struct{}), wireDef)
	aWire.IsReady = false
	aWire.Originator = HOST_CREATED_WIRE
	aWire.OriginatorIP = "unknown"

	// Add the newly created wire in the in memory wire-map and k8S data store
	AddWireInMemNDataStore(aWire, tapFile)

	log.WithFields(log.Fields{
		"daemon":  "meshnetd",
		"overlay": "gRPC",
	}).Infof("[ADD-WIRE:LOCAL-END] For pod %s@%s, wire id %d starting local packet receive thread", wireDef.LocalPodName, wireDef.IntfNameInPod, wireID)

	go RecvFrmLocalPodThread(aWire, aWire.LocalNodeIfaceName)

	return &mpb.BoolResponse{Response: true}, nil
}

// A remote peer can tell the local node to create/update the local end of the grpc-wire.
// At the local end if the wire is already created then update the wire properties.
// This updation can happen when a pod is deleted and recreated again. This is not very uncommon in K8S to move
// a pod from node A to node B dynamically.
// Returns the wire, whether it was freshly created (true) or updated (false), and any error.
func CreateUpdateGRPCWireRemoteTriggered(wireDef *mpb.WireDef, stopC chan struct{}) (*GRPCWire, bool, error) {

	// If this wire is already created, then only update the already created wire properties like stopC.
	// This can happen due to a race between the local and remote peer.
	// This can also happen when a pod in one end of the wire is deleted and created again.
	// In all cases link creation happen only once but it can get updated multiple times.
	grpcWire, ok := UpdateWireByUID(wireDef.LocalPodNetNs, int(wireDef.LinkUid), wireDef.WireIfIdOnPeerNode, stopC)
	if ok {
		grpcOvrlyLogger.Infof("[CREATE-UPDATE-WIRE] At remote end this grpc-wire is already created by %s. Local interface id : %d peer interface id : %d", grpcWire.Originator, grpcWire.LocalNodeIfaceID, grpcWire.WireIfaceIDOnPeerNode)
		return grpcWire, false, nil
	}

	tapFile, err := wireutil.CreateOrAttachTAP(wireDef.LocalPodNetNs, wireDef.IntfNameInPod, wireDef.LocalPodIp)
	if err != nil {
		grpcOvrlyLogger.Errorf("[ADD-WIRE:REMOTE-END] Error creating/attaching TAP interface %s in netns %s: %v",
			wireDef.IntfNameInPod, wireDef.LocalPodNetNs, err)
		return nil, false, err
	}

	wireID := NextIndex()
	grpcOvrlyLogger.Infof("[ADD-WIRE:REMOTE-END] Trigger from %s:%d : Successfully created/attached TAP interface %s@%s (wire id %d).",
		wireDef.PeerNodeIp, wireDef.WireIfIdOnPeerNode, wireDef.IntfNameInPod, wireDef.LocalPodName, wireID)

	aWire := CreateGWire(int(wireID), wireDef.IntfNameInPod, stopC, wireDef)

	// Add the created wire in the in memory wire-map and k8S data store
	AddWireInMemNDataStore(aWire, tapFile)

	return aWire, true, nil
}

// When the remote peer tells the local node to remove the local end of the grpc-wire info
func GRPCWireDownRemoteTriggered(wireDef *mpb.WireDef) error {

	err := WireDownByUID(wireDef.LocalPodNetNs, int(wireDef.LinkUid))
	if err != nil {
		grpcOvrlyLogger.Infof("[WIRE-DOWN] Remote end failed in making down wire end in pod %s@%s,. Link uid : %d",
			wireDef.LocalPodName, wireDef.IntfNameInPod, wireDef.LinkUid)
		return nil
	}

	return nil
}
