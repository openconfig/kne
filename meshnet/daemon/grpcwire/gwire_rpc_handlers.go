package grpcwire

import (
	"context"
	"fmt"
	"net"

	log "github.com/sirupsen/logrus"

	"github.com/containernetworking/plugins/pkg/ns"
	"github.com/google/gopacket/pcap"
	mpb "github.com/networkop/meshnet-cni/daemon/proto/meshnet/v1beta1"
	"github.com/networkop/meshnet-cni/utils/wireutil"
	koko "github.com/redhat-nfvpe/koko/api"
)

func CreateGRPCWireLocal(ctx context.Context, wireDef *mpb.WireDef) (*mpb.BoolResponse, error) {
	locInf, err := net.InterfaceByName(wireDef.WireIfNameOnLocalNode)
	if err != nil {
		log.WithFields(log.Fields{
			"daemon":  "meshnetd",
			"overlay": "gRPC",
		}).Errorf("[ADD-WIRE:LOCAL-END]For pod %s failed to retrieve interface ID for interface %v. error:%v", wireDef.LocalPodName, wireDef.WireIfNameOnLocalNode, err)
		return &mpb.BoolResponse{Response: false}, err
	}

	// update tx checksuming to off
	err = wireutil.SetTxChecksumOff(wireDef.IntfNameInPod, wireDef.LocalPodNetNs)
	if err != nil {
		log.Errorf("Error in setting tx checksum-off on interface %s, ns %s, pod %s: %v", wireDef.IntfNameInPod, wireDef.LocalPodNetNs, wireDef.LocalPodName, err)
		// generate error and continue
	} else {
		log.Infof("Setting tx checksum-off on interface %s, pod %s is successful", wireDef.IntfNameInPod, wireDef.LocalPodName)
	}

	//Using google gopacket for packet receive. An alternative could be using socket. Not sure it it provides any advantage over gopacket.
	wrHandle, err := pcap.OpenLive(wireDef.WireIfNameOnLocalNode, 65365, true, pcap.BlockForever)
	if err != nil {
		log.WithFields(log.Fields{
			"daemon":  "meshnetd",
			"overlay": "gRPC",
		}).Errorf("[ADD-WIRE:LOCAL-END]Could not open interface for send/recv packets for containers local iface id %d. error:%v", locInf.Index, err)
		return &mpb.BoolResponse{Response: false}, err
	}

	aWire := CreateGWire(locInf.Index, wireDef.WireIfNameOnLocalNode, make(chan struct{}), wireDef)
	aWire.IsReady = false
	aWire.Originator = HOST_CREATED_WIRE
	aWire.OriginatorIP = "unknown"

	// Add the newly created wire in the in memory wire-map and k8S data store
	AddWireInMemNDataStore(aWire, wrHandle)

	log.WithFields(log.Fields{
		"daemon":  "meshnetd",
		"overlay": "gRPC",
	}).Infof("[ADD-WIRE:LOCAL-END]For pod %s@%s, node iface id %d starting the local packet receive thread", wireDef.LocalPodName, wireDef.IntfNameInPod, locInf.Index)
	// TODO: handle error here
	go RecvFrmLocalPodThread(aWire, aWire.LocalNodeIfaceName)

	return &mpb.BoolResponse{Response: true}, nil
}

// A remote peer can tell the local node to create/update the local end of the grpc-wire.
// At the local end if the wire is already created then update the wire properties.
// This updation can happen when a pod is deleted and recreated again. This is not very uncommon in K8S to move
// a pod from node A to node B dynamically
func CreateUpdateGRPCWireRemoteTriggered(wireDef *mpb.WireDef, stopC chan struct{}) (*GRPCWire, error) {

	var err error

	// If this wire is already created, then only update the already created wire properties like stopC.
	// This can happen due to a race between the local and remote peer.
	// This can also happen when a pod in one end of the wire is deleted and created again.
	// In all cases link creation happen only once but it can get updated multiple times.
	grpcWire, ok := UpdateWireByUID(wireDef.LocalPodNetNs, int(wireDef.LinkUid), wireDef.WireIfIdOnPeerNode, stopC)
	if ok {
		grpcOvrlyLogger.Infof("[CREATE-UPDATE-WIRE] At remote end this grpc-wire is already created by %s. Local interface id : %d peer interface id : %d", grpcWire.Originator, grpcWire.LocalNodeIfaceID, grpcWire.WireIfaceIDOnPeerNode)
		return grpcWire, nil
	}

	outIfNm, err := GenNodeIfaceName(wireDef.LocalPodName, wireDef.IntfNameInPod)
	if err != nil {
		return nil, fmt.Errorf("[ADD-WIRE:REMOTE-END] could not get current network namespace: %v", err)
	}

	currNs, err := ns.GetCurrentNS()
	if err != nil {
		return nil, fmt.Errorf("[ADD-WIRE:REMOTE-END] could not get current network namespace: %v", err)
	}

	/* Create the veth to connect the pod with the meshnet daemon running on the node */
	hostEndVeth := koko.VEth{
		NsName:   currNs.Path(),
		LinkName: outIfNm,
	}

	inIfNm := wireDef.IntfNameInPod
	inContainerVeth := koko.VEth{
		NsName:   wireDef.LocalPodNetNs,
		LinkName: inIfNm,
	}

	if wireDef.LocalPodIp != "" {
		ipAddr, ipSubnet, err := net.ParseCIDR(wireDef.LocalPodIp)
		if err != nil {
			return nil, fmt.Errorf("failed to create remote end of GRPC wire(%s@%s), failed to parse CIDR %s: %w",
				inIfNm, wireDef.LocalPodName, wireDef.LocalPodIp, err)
		}
		inContainerVeth.IPAddr = []net.IPNet{{
			IP:   ipAddr,
			Mask: ipSubnet.Mask,
		}}
	}

	if err = koko.MakeVeth(inContainerVeth, hostEndVeth); err != nil {
		grpcOvrlyLogger.Errorf("[ADD-WIRE:REMOTE-END] Error creating vEth pair (in:%s <--> out:%s).  Error-> %s", inIfNm, outIfNm, err)
		return nil, err
	}
	if err := wireutil.SetTxChecksumOff(inContainerVeth.LinkName, inContainerVeth.NsName); err != nil {
		grpcOvrlyLogger.Errorf("Error in setting tx checksum-off on interface %s, pod %s: %v", inContainerVeth.LinkName, wireDef.LocalPodName, err)
		// not returning
	}
	locIface, err := net.InterfaceByName(hostEndVeth.LinkName)
	if err != nil {
		// let the caller handle the error
		grpcOvrlyLogger.Errorf("[ADD-WIRE:REMOTE-END] Remote end could not get interface index for %s. error:%v", hostEndVeth.LinkName, err)
		return nil, err
	}
	grpcOvrlyLogger.Infof("[ADD-WIRE:REMOTE-END] Trigger from %s:%d : Successfully created remote pod to node vEth pair %s@%s <--> %s(%d).",
		wireDef.PeerNodeIp, wireDef.WireIfIdOnPeerNode, inIfNm, wireDef.LocalPodName, outIfNm, locIface.Index)
	aWire := CreateGWire(locIface.Index, hostEndVeth.LinkName, stopC, wireDef)
	/* Utilizing google gopacket for polling for packets from the node. This seems to be the
	   simplest way to get all packets.
	   As an alternative to google gopacket(pcap), a socket based implementation is possible.
	   Not sure if socket based implementation can bring any advantage or not.

	   Near term will replace pcap by socket.
	*/
	wrHandle, err := pcap.OpenLive(hostEndVeth.LinkName, 65365, true, pcap.BlockForever)
	if err != nil {
		// let the caller handle the error
		grpcOvrlyLogger.Errorf("[ADD-WIRE:REMOTE-END] At remote end could not open interface (%d) for sed/recv packets for containers. error:%v", locIface.Index, err)
		return nil, err
	}

	// Add the created wire in the in memory wire-map and k8S data store
	AddWireInMemNDataStore(aWire, wrHandle)

	return aWire, nil
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
