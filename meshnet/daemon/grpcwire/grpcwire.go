package grpcwire

import (
	"context"
	"fmt"
	"io"
	"net"
	"strings"
	"sync"

	"github.com/google/gopacket"
	"github.com/google/gopacket/pcap"
	"github.com/openconfig/gnmi/errlist"
	koko "github.com/redhat-nfvpe/koko/api"
	log "github.com/sirupsen/logrus"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"

	mpb "github.com/networkop/meshnet-cni/daemon/proto/meshnet/v1beta1"
	"github.com/networkop/meshnet-cni/utils/wireutil"
)

var grpcOvrlyLogger *log.Entry = nil

func InitLogger() {
	grpcOvrlyLogger = log.WithFields(log.Fields{"daemon": "meshnetd", "overlay": "gRPC"})
}

type intfIndex struct {
	mu     sync.Mutex
	currId int64
}

/*
	In a given node a veth-pair connects a pod with the meshnet daemon hosted in the node. This meshnet

daemon provides the grpc-wire service to connect the local pod with the remote pod over grpc. The node
end of the veth-pair must have unique name with in the node. A node can have multiple pods. So there
will be multiple veth-pairs for connecting multiple nodes to meshnet daemon and each of them (the node end) must have unique
names. IntfIndex provides the sequentially increasing number which makes the name unique when added as
suffix to the name.
*/
var indexGen intfIndex

func NextIndex() int64 {
	indexGen.mu.Lock()
	defer indexGen.mu.Unlock()
	indexGen.currId++
	return indexGen.currId
}

/*+++tbf: These constants has no utility other that helping in debugging. These can be removed later. */
type grpcWireOriginator int

func (g grpcWireOriginator) String() string {
	switch g {
	case HOST_CREATED_WIRE:
		return "host originated"
	case PEER_CREATED_WIRE:
		return "peer originated"
	}
	return "unknown originator"
}

const (
	HOST_CREATED_WIRE grpcWireOriginator = iota
	PEER_CREATED_WIRE
)

type GRPCWire struct {
	UID           int    // uid identify a particular link in a topology as per meshnet crd
	TopoNamespace string // K8s namespace this wire belongs to

	/* Node information */
	LocalNodeIfaceID   int64  // OS assigned interface ID of local node interface
	LocalNodeIfaceName string // name of local node interface

	/* Pod information : where this wire is terminating in this node */
	LocalPodIP        string // IP address of the local container who will consume packets over this wire.
	LocalPodName      string // Name the local pod who will consume packets over this wire.
	LocalPodIfaceName string // Name the interface which is inside the local pod who will consume packets over this wire. This is for debugging
	LocalPodNetNS     string

	/*Peer pod information*/
	WireIfaceIDOnPeerNode int64  // Peer end of the wire interface ID which is present in peer node
	PeerNodeIP            string // Peer node IP

	IsReady      bool               // Is this wire ip.
	Originator   grpcWireOriginator // create by local host or create on trigger from remote host. This is for debugging.
	OriginatorIP string             // IP address of the host created it. This is for debugging.

	StopC chan struct{} // the channel to send stop signal to the receive thread.
	mu    sync.Mutex
}

type linkKey struct {
	namespace string
	linkUID   int
}

func CreateGWire(locIfIndex int, locIfNm string, stopC chan struct{}, wireDef *mpb.WireDef) *GRPCWire {

	return &GRPCWire{
		UID: int(wireDef.LinkUid),

		LocalNodeIfaceID:   int64(locIfIndex),
		LocalNodeIfaceName: locIfNm,
		LocalPodIP:         wireDef.LocalPodIp,
		LocalPodIfaceName:  wireDef.IntfNameInPod,
		LocalPodName:       wireDef.LocalPodName,
		LocalPodNetNS:      wireDef.LocalPodNetNs,

		WireIfaceIDOnPeerNode: wireDef.WireIfIdOnPeerNode,
		PeerNodeIP:            wireDef.PeerNodeIp,

		IsReady:      true,
		Originator:   PEER_CREATED_WIRE,
		OriginatorIP: wireDef.PeerNodeIp,

		StopC:         stopC,
		TopoNamespace: wireDef.TopoNs,
	}

}

// update the ware with the given input and mark the wire ready
func (wire *GRPCWire) UpdateWire(peerIntfId int64, stopC chan struct{}) {
	wire.mu.Lock()
	defer wire.mu.Unlock()
	wire.StopC = stopC
	if !wire.IsReady {
		wire.WireIfaceIDOnPeerNode = peerIntfId
	}
	wire.IsReady = true
}

// Delete a wire from the in-memory wire-map under a lock
func (w *wireMap) Delete(wire *GRPCWire) error {
	w.mu.Lock()
	defer w.mu.Unlock()
	err := w.DeleteWoLock(wire)
	return err
}

// GetWireByUID returns wire matching the provided namespace and linkUID.
func GetWireByUID(namespace string, linkUID int) (*GRPCWire, bool) {
	return wires.GetWire(namespace, linkUID)
}

// For the given uid if the wire exists, then update the wire properties.
// Returns true if a wire exists, also the wire structure that got modified
func UpdateWireByUID(namespace string, linkUID int, peerIntfId int64, stopC chan struct{}) (*GRPCWire, bool) {
	wires.mu.Lock()
	defer wires.mu.Unlock()
	wire, ok := wires.wires[linkKey{
		namespace: namespace,
		linkUID:   linkUID,
	}]
	if ok {
		wire.StopC = stopC
		if !wire.IsReady {
			wire.WireIfaceIDOnPeerNode = peerIntfId
		}
		wire.IsReady = true
	}
	return wire, ok
}

// WireDownByUID - stops packet collection from the connected pod
func WireDownByUID(namespace string, linkUID int) error {
	wires.mu.Lock()
	defer wires.mu.Unlock()

	wire, ok := wires.wires[linkKey{
		namespace: namespace,
		linkUID:   linkUID,
	}]
	if ok {
		grpcOvrlyLogger.Infof("WireDownByUID: Making wire down from db, %s@%s-%s@%d, peer fid %d, link uid %d",
			wire.LocalPodName, wire.LocalPodIfaceName, wire.LocalNodeIfaceName, wire.LocalNodeIfaceID, wire.WireIfaceIDOnPeerNode, linkUID)
		if wire.IsReady {
			close(wire.StopC)
		}
		wire.IsReady = false
	} else {
		grpcOvrlyLogger.Infof("WireDownByUID: Did not find entry to make down from db, uid %d, ns %s",
			linkUID, namespace)
	}
	return nil
}

// -------------------------------------------------------------------------------------------------
func AddWireInMemNDataStore(wire *GRPCWire, handle *pcap.Handle) int {
	/* Populate the active wire map and returns the number of currently added active wires. */

	/* if this wire is already present in the map then it will be overwritten.
	   It seems to be ok to overwrite. Think more in what situation this may
	   not be the desired behavior and we need to throw an error. */
	wire.IsReady = true

	wires.AddInMemNDataStore(wire, handle)
	return len(wires.wires)
}

// -------------------------------------------------------------------------------------------------
// DeleteWire cleans up the active wire map and returns the number of currently added active wire.
func DeleteWire(wire *GRPCWire) int {
	wires.AtomicDelete(wire)
	return len(wires.wires)
}

// -----------------------------------------------------------------------------------------------------------
// This function is used for delete operation. It deletes all the wires connected with the pod.
// This function clear up the in-memory data base as well as the K8S Datastore.
func DeletePodWires(namespace string, podName string) error {
	var errs errlist.List
	for {
		aW, _ := ExtractOneWireByPod(namespace, podName)
		if aW == nil {
			break
		}

		// Since this wire is already extracted, so it no more preset in in-memory-map. Next we need to clear only the K8S data store.
		if err := RemoveWireAcrosAll(aW, false); err != nil {
			grpcOvrlyLogger.Infof("[WIRE-DELETE]:Error Removing local-iface@pod : %s@%s for wire UID: %d, iface id %d : %v", aW.LocalPodIfaceName, aW.LocalPodName, aW.UID, aW.LocalNodeIfaceID, err)
			errs.Add(err)
		} else {
			grpcOvrlyLogger.Infof("[WIRE-DELETE]:Removed local-iface@pod : %s@%s for wire UID: %d, iface id %d", aW.LocalPodIfaceName, aW.LocalPodName, aW.UID, aW.LocalNodeIfaceID)
		}
	}
	if errs.Err() != nil {
		return fmt.Errorf("[WIRE-DELETE]:failed to remove all grpc-wires for pod %s@%s: %w", podName, namespace, errs.Err())
	}
	grpcOvrlyLogger.Infof("[WIRE-DELETE]:All grpc-wires for pod %s:%s is deleted", namespace, podName)
	return nil
}

// ----------------------------------------------------------------------------------------------------------
// Cleanup function for clearing up the in-memory wire map anf the K8S data store, when the meshnet cni plugin
// instructs the meshenet daemon to destroy a wire. Before deleting this function stops the thread for receiving
// packets from the pod connected to this wire.
// input parameter imMem set to true to clear the in-memory wire map.
func RemoveWireAcrosAll(wire *GRPCWire, inMem bool) error {

	if wire == nil {
		grpcOvrlyLogger.Infof("[WIRE-DELETE]:Null wire. This ware is already removed")
		return nil
	}

	// stop the packet receive thread for this pod
	if wire.IsReady {
		close(wire.StopC)
	}
	wire.IsReady = false

	/* Remove the veth from the node */
	intf, err := net.InterfaceByIndex(int(wire.LocalNodeIfaceID))
	if err != nil {
		grpcOvrlyLogger.Infof("[WIRE-DELETE]:Interface index %d for wire %d, is already cleaned up.", wire.LocalNodeIfaceID, wire.UID)
	} else {
		myVeth := koko.VEth{}
		myVeth.LinkName = intf.Name
		if err = myVeth.RemoveVethLink(); err != nil {
			return fmt.Errorf("[WIRE-DELETE]:failed to remove veth link: %w", err)
		}
	}

	// clean up im-memory wire-map
	if inMem {
		wires.AtomicDelete(wire) // Deleting the wire from in-memory data
	}
	//delete from data-store
	wire.K8sDelGWire()
	grpcOvrlyLogger.Infof("[WIRE-DELETE]:Successfully removed grpc wire for link %d, iface id %d.", wire.UID, wire.LocalNodeIfaceID)
	return nil
}

// -----------------------------------------------------------------------------------------------------------
// Generate the name of the interface to be placed on the node
func GenNodeIfaceName(podName string, podIfaceName string) (string, error) {
	// Linux has issue if interface name is too long. Generate a smaller name.
	// In recent kernel versions this is defined by IFNAMSIZ to be 16 bytes, so 15 user-visible bytes
	// (assuming it includes a trailing null). IFNAMSIZ is used in defining struct net_device's name.
	// The name must not contain / or any whitespace characters
	//
	//TODO: This method needs to be robust. It monotonically increases the index and never
	//      decreases it, even if the interfaces are deleted. So far this will work for accumulated
	//      1K interfaces per node under the current naming scheme. This is too small.
	//      Using 14 digit random number and checking if any interface with generated name exists and if
	//      exists then generate another random number (try 3 times before giving up). This will make it robust.
	//      This reduces the readability and corelation between the “pod-interface” and corresponding
	//      “node-interface”, for example eth1host1-<3-digit-index> will become "12345678901234".
	id := NextIndex()

	ifaceName := fmt.Sprintf("%.5s%.5s-%04d", podName, podIfaceName, id)

	return ifaceName, nil
}

// -----------------------------------------------------------------------------------------------------------
func RecvFrmLocalPodThread(wire *GRPCWire, locIfNm string) error {

	defaultPort := wireutil.GRPCDefaultPort
	pktBuffSz := int32(1024 * 64 * 10) //keep buffer for MAX 10 64K frames

	url := strings.TrimSpace(fmt.Sprintf("%s:%d", wire.PeerNodeIP, defaultPort))
	/* Utilizing google gopacket for polling for packets from the node. This seems to be the
	   simplest way to get all packets.
	   As an alternative to google gopacket(pcap), a socket based implementation is possible.
	   Not sure if socket based implementation can bring any advantage or not.

	   Near term will replace pcap by socket.
	*/

	// in some rare cases by the time the thread starts K8S may decide to move the pod somewhere else.
	// in that case the local interfaced will be cleaned up asynchronously. Detect the situation and return.
	_, err := net.InterfaceByName(locIfNm)
	if err != nil {
		grpcOvrlyLogger.Errorf("[Packet Receive thread]For pod %s failed to retrieve interface %s/%d. error: %v", wire.LocalPodName, wire.LocalNodeIfaceName, wire.LocalNodeIfaceID, err)
		return err
	}

	rdHandl, err := pcap.OpenLive(wire.LocalNodeIfaceName, pktBuffSz, true, pcap.BlockForever)
	if err != nil {
		// let the caller handle the error
		grpcOvrlyLogger.Errorf("Receive Thread for local pod failed to open interface: %s/%d, PCAP ERROR: %v", wire.LocalNodeIfaceName, wire.LocalNodeIfaceID, err)
		return err
	}
	defer rdHandl.Close()

	err = rdHandl.SetDirection(pcap.Direction(pcap.DirectionIn))
	if err != nil {
		// let the caller handle the error
		grpcOvrlyLogger.Errorf("Receive Thread for local pod failed to set up capture direction: %s/%d, PCAP ERROR: %v", wire.LocalNodeIfaceName, wire.LocalNodeIfaceID, err)
		return err
	}

	remote, err := grpc.Dial(url, grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		grpcOvrlyLogger.Infof("RecvFrmLocalPodThread:Failed to connect to remote %s/%d", url, wire.LocalNodeIfaceID)
		return err
	}
	defer remote.Close()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	source := gopacket.NewPacketSource(rdHandl, rdHandl.LinkType())
	wireClient := mpb.NewWireProtocolClient(remote)

	in := source.Packets()
	var packet gopacket.Packet
	for {
		select {
		case <-wire.StopC:
			grpcOvrlyLogger.Infof("RecvFrmLocalPodThread: closing connection with remote peer-iface@peer-node-ip: %d@%s/%d from %s@%s",
				wire.WireIfaceIDOnPeerNode, wire.PeerNodeIP, wire.LocalNodeIfaceID, wire.LocalPodName, wire.LocalPodIfaceName)
			return io.EOF
		case packet = <-in:
			data := packet.Data()
			payload := &mpb.Packet{
				RemotIntfId: wire.WireIfaceIDOnPeerNode,
				Frame:       data,
			}

			/*+++TODO: Ethernet has a minimum frame size of 64 bytes, comprising an 18-byte header and a payload of 46 bytes.
			It also has a maximum frame size of 1518 bytes, in which case the payload is 1500 bytes.
			This logic needs to be better, take the interface MTU not hardcoded value of 1518.
			This is a very unusual condition to receive an packet from the pod with size > MTU. This can only happens if
			things gets really messed up.   */
			if len(data) > 1518 {
				pktType := DecodeFrame(payload.Frame)
				grpcOvrlyLogger.Infof("RecvFrmLocalPodThread: unusually large packet received from local pod (may be GRO enabled). size: %d, pkt:%s", len(data), pktType)
				/* When Generic Receive Offload (GRO) is enabled then containers can send packets larger than MTU size packet. Do not drop these
				   packets, deliver it to the receiving container to process.
				*/
				//continue
			}

			ok, err := wireClient.SendToOnce(ctx, payload)
			if err != nil || !ok.Response {
				grpcOvrlyLogger.Infof("RecvFrmLocalPodThread: Could not deliver pkt %s@%s@%s. Peer not ready, remote iface id %d. err=%v",
					wire.LocalPodName, wire.LocalPodIfaceName, wire.LocalNodeIfaceName, wire.WireIfaceIDOnPeerNode, err)
				/* we generate information and continue. As the above errors will happen when the remote end is not yet ready.
				   It will eventually get ready and if it can't then someone else will stop this thread.
				*/
			}
		}
	}
}
