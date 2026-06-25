package grpcwire

import (
	"fmt"
	"sync"

	"github.com/google/gopacket/pcap"
)

type wireMap struct {
	mu      sync.Mutex
	wires   map[linkKey]*GRPCWire
	handles map[int64]*pcap.Handle
}

func (w *wireMap) GetWire(namespace string, linkUID int) (*GRPCWire, bool) {
	w.mu.Lock()
	defer w.mu.Unlock()
	wire, ok := w.wires[linkKey{
		namespace: namespace,
		linkUID:   linkUID,
	}]
	return wire, ok
}

func (w *wireMap) GetHandle(key int64) (*pcap.Handle, bool) {
	w.mu.Lock()
	defer w.mu.Unlock()
	handle, ok := w.handles[key]
	return handle, ok
}

func (w *wireMap) AddInMem(wire *GRPCWire, handle *pcap.Handle) error {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.wires[linkKey{
		namespace: wire.LocalPodNetNS,
		linkUID:   wire.UID,
	}] = wire

	w.handles[wire.LocalNodeIfaceID] = handle
	return nil
}

func (w *wireMap) AddInMemNDataStore(wire *GRPCWire, handle *pcap.Handle) error {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.wires[linkKey{
		namespace: wire.LocalPodNetNS,
		linkUID:   wire.UID,
	}] = wire

	wire.K8sStoreGWire()

	w.handles[wire.LocalNodeIfaceID] = handle
	return nil
}

// Clear the in-memory wire map
func (w *wireMap) AtomicDelete(wire *GRPCWire) error {
	w.mu.Lock()
	defer w.mu.Unlock()
	delete(w.wires, linkKey{
		namespace: wire.LocalPodNetNS,
		linkUID:   wire.UID,
	})

	delete(w.handles, wire.LocalNodeIfaceID)

	return nil
}

// Delete a wire from the in-memory wire-map without a lock
func (w *wireMap) DeleteWoLock(wire *GRPCWire) error {
	delete(w.wires, linkKey{
		namespace: wire.LocalPodNetNS,
		linkUID:   wire.UID,
	})
	delete(w.handles, wire.LocalNodeIfaceID)
	return nil
}

/* A grpc-wire creation (between pod A and pod B) can be triggered by either host hosting pod A, B. They
 * can even trigger it simultaneously. Irrespective of who triggers, successful wire creation needs
 * activities at both hosts end. Our intention is to finish the wire creation at the first trigger.
 * This map keeps the list of wires which are already crated and must not be recreated, if any second
 * trigger is received. This situation occurs when both the host triggers wire creation almost simultaneously.
 */
var wires = &wireMap{
	wires: map[linkKey]*GRPCWire{},
	/* Used when a packet is received, then we know the id of the interface to which the packet to be delivered.
	   This map take interface-id as key and returns the corresponding handle for delivering the packet.
	   map[interface-id]->handle */
	handles: map[int64]*pcap.Handle{},
}

// FindWiresByPod returns a list of wires matching the namespace and pod.
func GetWiresByPod(namespace string, podName string) ([]*GRPCWire, bool) {
	wires.mu.Lock()
	defer wires.mu.Unlock()
	var rWires []*GRPCWire

	for _, wire := range wires.wires {
		if wire.LocalPodName == podName && wire.TopoNamespace == namespace {
			rWires = append(rWires, wire)
		}
	}
	return rWires, true
}

// For a given pod, this atomic function extracts and returns the first wire from the wire map. Note the wire is
// removed from the wire-map. This function is expected to be used for deleting and wire.
func ExtractOneWireByPod(namespace string, podName string) (*GRPCWire, bool) {
	wires.mu.Lock()
	defer wires.mu.Unlock()
	//var rWires *GRPCWire

	for _, wire := range wires.wires {
		if wire.LocalPodName == podName && wire.TopoNamespace == namespace {
			// delete this wire from wire map.
			delete(wires.wires, linkKey{
				namespace: wire.LocalPodNetNS,
				linkUID:   wire.UID,
			})

			// also clean up the pcap handle for the wire that is extracted from the wire-map
			delete(wires.handles, wire.LocalNodeIfaceID)
			return wire, true
		}
	}
	return nil, true // no wire found is not a failure, so return true
}

func GetHostIntfHndl(intfID int64) (*pcap.Handle, error) {

	val, ok := wires.GetHandle(intfID)
	if ok {
		return val, nil
	}
	return nil, fmt.Errorf("node interface %d is not found in local db", intfID)

}
