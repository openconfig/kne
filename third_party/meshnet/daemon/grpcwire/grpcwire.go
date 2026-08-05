// Package grpcwire provides gRPC overlay wire creation, TAP interface management,
// packet multiplexing, and CRD reconciliation for meshnet daemon.
package grpcwire

import (
	"context"
	"fmt"
	"io"
	"os"
	"strings"
	"sync"

	"github.com/containernetworking/plugins/pkg/ns"
	"github.com/openconfig/gnmi/errlist"
	log "github.com/sirupsen/logrus"
	"github.com/vishvananda/netlink"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"

	mpb "github.com/openconfig/kne/third_party/meshnet/daemon/proto/meshnet/v1beta1"
	"github.com/openconfig/kne/third_party/meshnet/utils/wireutil"
)

var grpcOvrlyLogger *log.Entry = nil

// InitLogger initializes logrus logging for the gRPC overlay daemon.
func InitLogger() {
	grpcOvrlyLogger = log.WithFields(log.Fields{"daemon": "meshnetd", "overlay": "gRPC"})
}

type intfIndex struct {
	mu     sync.Mutex
	currId int64
}

/*
	In a given node a TAP interface connects a pod with the meshnet daemon hosted in the node. This meshnet

daemon provides the grpc-wire service to connect the local pod with the remote pod over grpc. IntfIndex provides
the sequentially increasing number which makes the name unique when added as suffix to the name.
*/
var indexGen intfIndex

// NextIndex generates a node-wide, monotonically increasing unique wire ID for TAP device handle indexing.
func NextIndex() int64 {
	indexGen.mu.Lock()
	defer indexGen.mu.Unlock()
	indexGen.currId++
	return indexGen.currId
}

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

// CreateGWire constructs a new GRPCWire struct from the provided wire definition.
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

// AddWireInMemNDataStore populates the active wire map and updates K8s status store.
func AddWireInMemNDataStore(wire *GRPCWire, handle *os.File) int {
	/* Populate the active wire map and returns the number of currently added active wires. */
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
		grpcOvrlyLogger.Infof("[WIRE-DELETE]:Null wire. This wire is already removed")
		return nil
	}

	// stop the packet receive thread for this pod
	if wire.IsReady {
		close(wire.StopC)
	}
	wire.IsReady = false

	// Close the TAP file handle if open
	if handle, ok := wires.GetHandle(wire.LocalNodeIfaceID); ok && handle != nil {
		_ = handle.Close()
	}

	// Remove the TAP link from the container netns if present
	podNs, err := ns.GetNS(wire.LocalPodNetNS)
	if err == nil {
		_ = podNs.Do(func(_ ns.NetNS) error {
			if link, err := netlink.LinkByName(wire.LocalPodIfaceName); err == nil {
				return netlink.LinkDel(link)
			}
			return nil
		})
		podNs.Close()
	}

	// clean up in-memory wire-map
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
	id := NextIndex()
	ifaceName := fmt.Sprintf("%.5s%.5s-%04d", podName, podIfaceName, id)
	return ifaceName, nil
}

// RecvFrmLocalPodThread reads packets from the local TAP interface and forwards them over the gRPC stream.
func RecvFrmLocalPodThread(wire *GRPCWire, locIfNm string) error {

	defaultPort := wireutil.GRPCDefaultPort
	url := strings.TrimSpace(fmt.Sprintf("%s:%d", wire.PeerNodeIP, defaultPort))

	tapFile, err := GetHostIntfHndl(wire.LocalNodeIfaceID)
	if err != nil {
		grpcOvrlyLogger.Errorf("[Packet Receive thread] For pod %s failed to retrieve TAP handle for interface %s/%d. error: %v", wire.LocalPodName, wire.LocalNodeIfaceName, wire.LocalNodeIfaceID, err)
		return err
	}

	dialOpts := []grpc.DialOption{
		grpc.WithTransportCredentials(insecure.NewCredentials()),
		grpc.WithInitialWindowSize(4 * 1024 * 1024),     // 4MB stream window
		grpc.WithInitialConnWindowSize(16 * 1024 * 1024), // 16MB connection window
		grpc.WithDefaultCallOptions(
			grpc.MaxCallRecvMsgSize(64*1024*1024),
			grpc.MaxCallSendMsgSize(64*1024*1024),
		),
	}

	remote, err := grpc.Dial(url, dialOpts...)
	if err != nil {
		grpcOvrlyLogger.Infof("RecvFrmLocalPodThread:Failed to connect to remote %s/%d", url, wire.LocalNodeIfaceID)
		return err
	}
	defer remote.Close()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	wireClient := mpb.NewWireProtocolClient(remote)

	var stream mpb.WireProtocol_SendToStreamClient
	getStream := func() (mpb.WireProtocol_SendToStreamClient, error) {
		if stream != nil {
			return stream, nil
		}
		st, err := wireClient.SendToStream(ctx)
		if err != nil {
			return nil, err
		}
		stream = st
		return stream, nil
	}

	buf := make([]byte, 65535)
	type readResult struct {
		n   int
		err error
	}
	readChan := make(chan readResult, 1)
	go func() {
		for {
			n, err := tapFile.Read(buf)
			readChan <- readResult{n: n, err: err}
			if err != nil {
				return
			}
		}
	}()

	for {
		select {
		case <-wire.StopC:
			grpcOvrlyLogger.Infof("RecvFrmLocalPodThread: closing connection with remote peer-iface@peer-node-ip: %d@%s/%d from %s@%s",
				wire.WireIfaceIDOnPeerNode, wire.PeerNodeIP, wire.LocalNodeIfaceID, wire.LocalPodName, wire.LocalPodIfaceName)
			if stream != nil {
				_, _ = stream.CloseAndRecv()
			}
			return io.EOF
		case res := <-readChan:
			if res.err != nil {
				select {
				case <-wire.StopC:
					return io.EOF
				default:
					grpcOvrlyLogger.Errorf("RecvFrmLocalPodThread: error reading from TAP interface %s: %v", locIfNm, res.err)
					return res.err
				}
			}
			n := res.n
			if n <= 0 {
				continue
			}

			frame := make([]byte, n)
			copy(frame, buf[:n])

			if !wire.IsReady || wire.WireIfaceIDOnPeerNode <= 0 {
				// Remote peer handshake is still in progress; skip sending to unassigned wire ID 0
				continue
			}

			payload := &mpb.Packet{
				RemotIntfId: wire.WireIfaceIDOnPeerNode,
				Frame:       frame,
			}

			if n > 1518 {
				pktType := DecodeFrame(payload.Frame)
				grpcOvrlyLogger.Infof("RecvFrmLocalPodThread: unusually large packet received from local pod (may be GRO enabled). size: %d, pkt:%s", n, pktType)
			}

			st, err := getStream()
			if err != nil {
				grpcOvrlyLogger.Debugf("RecvFrmLocalPodThread: Could not get stream for %s@%s: %v", wire.LocalPodName, wire.LocalNodeIfaceName, err)
				continue
			}

			if err := st.Send(payload); err != nil {
				grpcOvrlyLogger.Debugf("RecvFrmLocalPodThread: Could not send packet over stream %s@%s: %v", wire.LocalPodName, wire.LocalNodeIfaceName, err)
				stream = nil // reset stream for reconnect on next packet
			}
		}
	}
}
