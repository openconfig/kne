package grpcwire

import (
	"fmt"
	"os"
	"sync"
)

type wireMap struct {
	mu      sync.Mutex
	wires   map[linkKey]*GRPCWire
	handles map[int64]*os.File
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

func (w *wireMap) GetHandle(key int64) (*os.File, bool) {
	w.mu.Lock()
	defer w.mu.Unlock()
	handle, ok := w.handles[key]
	return handle, ok
}

func (w *wireMap) AddInMem(wire *GRPCWire, handle *os.File) error {
	w.mu.Lock()
	defer w.mu.Unlock()

	for key, oldWire := range w.wires {
		if oldWire.TopoNamespace == wire.TopoNamespace &&
			oldWire.LocalPodName == wire.LocalPodName &&
			oldWire.UID == wire.UID &&
			oldWire.LocalPodNetNS != wire.LocalPodNetNS {
			if oldWire.IsReady {
				close(oldWire.StopC)
				oldWire.IsReady = false
			}
			if oldHandle, ok := w.handles[oldWire.LocalNodeIfaceID]; ok && oldHandle != nil {
				_ = oldHandle.Close()
				delete(w.handles, oldWire.LocalNodeIfaceID)
			}
			delete(w.wires, key)
		}
	}

	w.wires[linkKey{
		namespace: wire.LocalPodNetNS,
		linkUID:   wire.UID,
	}] = wire

	w.handles[wire.LocalNodeIfaceID] = handle
	return nil
}

func (w *wireMap) AddInMemNDataStore(wire *GRPCWire, handle *os.File) error {
	w.mu.Lock()
	w.wires[linkKey{
		namespace: wire.LocalPodNetNS,
		linkUID:   wire.UID,
	}] = wire
	w.handles[wire.LocalNodeIfaceID] = handle
	w.mu.Unlock()

	go wire.K8sStoreGWire()
	return nil
}

// CloseAndRemoveHandle closes the TAP file handle for the given interface ID and removes it from the map.
func (w *wireMap) CloseAndRemoveHandle(key int64) error {
	w.mu.Lock()
	defer w.mu.Unlock()
	if handle, ok := w.handles[key]; ok {
		delete(w.handles, key)
		if handle != nil {
			return handle.Close()
		}
	}
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

	if handle, ok := w.handles[wire.LocalNodeIfaceID]; ok {
		delete(w.handles, wire.LocalNodeIfaceID)
		if handle != nil {
			_ = handle.Close()
		}
	}

	return nil
}

/* A grpc-wire creation (between pod A and pod B) can be triggered by either host hosting pod A, B. They
 * can even trigger it simultaneously. Irrespective of who triggers, successful wire creation needs
 * activities at both hosts end. Our intention is to finish the wire creation at the first trigger.
 * This map keeps the list of wires which are already created and must not be recreated, if any second
 * trigger is received. This situation occurs when both the host triggers wire creation almost simultaneously.
 */
var wires = &wireMap{
	wires:   map[linkKey]*GRPCWire{},
	handles: map[int64]*os.File{},
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

	for _, wire := range wires.wires {
		if wire.LocalPodName == podName && wire.TopoNamespace == namespace {
			// delete this wire from wire map.
			delete(wires.wires, linkKey{
				namespace: wire.LocalPodNetNS,
				linkUID:   wire.UID,
			})

			return wire, true
		}
	}
	return nil, true // no wire found is not a failure, so return true
}

func GetHostIntfHndl(intfID int64) (*os.File, error) {

	val, ok := wires.GetHandle(intfID)
	if ok {
		return val, nil
	}
	return nil, fmt.Errorf("node interface %d is not found in local db", intfID)

}
