package grpcwire

import (
	"testing"
)

func TestAddInMem_StaleWireCleanup(t *testing.T) {
	stopC1 := make(chan struct{})
	w1 := &GRPCWire{
		UID:           101,
		TopoNamespace: "default",
		LocalPodName:  "pod1",
		LocalPodNetNS: "/proc/111/ns/net",
		IsReady:       true,
		StopC:         stopC1,
	}

	wires.AddInMem(w1, nil)

	if wire, ok := GetWireByUID("/proc/111/ns/net", 101); !ok || wire != w1 {
		t.Fatalf("expected w1 in wires map, got ok=%t", ok)
	}

	w2 := &GRPCWire{
		UID:           101,
		TopoNamespace: "default",
		LocalPodName:  "pod1",
		LocalPodNetNS: "/proc/222/ns/net",
		IsReady:       true,
		StopC:         make(chan struct{}),
	}

	wires.AddInMem(w2, nil)

	// Old wire for /proc/111/ns/net should be deleted and its StopC closed
	if _, ok := GetWireByUID("/proc/111/ns/net", 101); ok {
		t.Fatalf("expected old wire /proc/111/ns/net to be deleted")
	}

	select {
	case <-stopC1:
		// expected: stopC1 was closed
	default:
		t.Fatalf("expected old wire StopC to be closed")
	}

	// New wire should be present
	if wire, ok := GetWireByUID("/proc/222/ns/net", 101); !ok || wire != w2 {
		t.Fatalf("expected w2 in wires map for /proc/222/ns/net, got ok=%t", ok)
	}

	// Clean up
	wires.AtomicDelete(w2)
}
