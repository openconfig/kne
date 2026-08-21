package grpcwire

import (
	"context"
	"testing"

	mpb "github.com/openconfig/kne/third_party/meshnet/daemon/proto/meshnet/v1beta1"
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

func TestUpdateWireByUID_PeerIPUpdate(t *testing.T) {
	w := &GRPCWire{
		UID:                   202,
		TopoNamespace:         "default",
		LocalPodName:          "podA",
		LocalPodNetNS:         "/proc/555/ns/net",
		WireIfaceIDOnPeerNode: 100,
		PeerNodeIP:            "10.0.0.2",
		IsReady:               true,
	}

	wires.AddInMem(w, nil)

	// Update with new peer interface ID and new peer node IP (e.g. peer rescheduled to 10.0.0.3)
	updated, ok := UpdateWireByUID("/proc/555/ns/net", 202, 300, "10.0.0.3", make(chan struct{}))
	if !ok || updated == nil {
		t.Fatalf("expected wire to be found and updated")
	}

	if updated.PeerNodeIP != "10.0.0.3" {
		t.Fatalf("expected PeerNodeIP to be updated to 10.0.0.3, got %s", updated.PeerNodeIP)
	}

	if updated.WireIfaceIDOnPeerNode != 300 {
		t.Fatalf("expected WireIfaceIDOnPeerNode to be updated to 300, got %d", updated.WireIfaceIDOnPeerNode)
	}

	// Clean up
	wires.AtomicDelete(w)
}

func TestAddInMemNDataStore_StaleWireCleanup(t *testing.T) {
	// Override k8s client interface so K8sStoreGWire doesn't crash on nil client
	SetGWireClientInterface(nil)
	stopC1 := make(chan struct{})
	w1 := &GRPCWire{
		UID:           303,
		TopoNamespace: "default",
		LocalPodName:  "pod2",
		LocalPodNetNS: "/proc/333/ns/net",
		IsReady:       false,
		StopC:         stopC1,
	}

	wires.AddInMemNDataStore(w1, nil)

	if wire, ok := GetWireByUID("/proc/333/ns/net", 303); !ok || wire != w1 {
		t.Fatalf("expected w1 in wires map, got ok=%t", ok)
	}

	w2 := &GRPCWire{
		UID:           303,
		TopoNamespace: "default",
		LocalPodName:  "pod2",
		LocalPodNetNS: "/proc/444/ns/net",
		IsReady:       true,
		StopC:         make(chan struct{}),
	}

	wires.AddInMemNDataStore(w2, nil)

	if _, ok := GetWireByUID("/proc/333/ns/net", 303); ok {
		t.Fatalf("expected old wire /proc/333/ns/net to be cleaned up by AddInMemNDataStore")
	}

	select {
	case <-stopC1:
	default:
		t.Fatalf("expected old wire StopC to be closed even if IsReady was false")
	}

	wires.AtomicDelete(w2)
}

func TestCloseStopC_Idempotent(t *testing.T) {
	stopC := make(chan struct{})
	w := &GRPCWire{
		UID:   404,
		StopC: stopC,
	}

	// Calling CloseStopC multiple times should be safe and idempotent (no panic)
	w.CloseStopC()
	w.CloseStopC()
	w.CloseStopC()

	select {
	case <-stopC:
		// channel closed as expected
	default:
		t.Fatalf("expected stopC to be closed")
	}
}

func TestWireDownThenRemove_NoDoubleClosePanic(t *testing.T) {
	w := &GRPCWire{
		UID:           505,
		TopoNamespace: "default",
		LocalPodName:  "podX",
		LocalPodNetNS: "/proc/505/ns/net",
		IsReady:       true,
		StopC:         make(chan struct{}),
	}
	wires.AddInMem(w, nil)

	// 1. Remote tells local node wire is down
	if err := WireDownByUID("/proc/505/ns/net", 505); err != nil {
		t.Fatalf("WireDownByUID failed: %v", err)
	}

	// 2. Later local pod is destroyed and RemoveWireAcrosAll is called
	if err := RemoveWireAcrosAll(w, true); err != nil {
		t.Fatalf("RemoveWireAcrosAll failed: %v", err)
	}
}

func TestStreamManager_SendUnregisteredReturnsFalse(t *testing.T) {
	// Calling Send on a non-existent stream should return false without creating a leaked stream
	key := nodeStreamKey{topoNs: "unregistered-ns", peerIP: "192.0.2.1"}
	if streamMgr.Send(key.topoNs, key.peerIP, nil) {
		t.Fatalf("expected Send to return false for nil packet")
	}

	streamMgr.mu.Lock()
	_, exists := streamMgr.streams[key]
	streamMgr.mu.Unlock()

	if exists {
		t.Fatalf("expected streamMgr.Send not to register unowned stream")
	}
}

func TestNodeStream_StopIdempotent(t *testing.T) {
	st := &NodeStream{
		key:      nodeStreamKey{topoNs: "test-topo", peerIP: "192.0.2.2"},
		pktChan:  make(chan *mpb.Packet, 10),
		stopChan: make(chan struct{}),
	}

	// Multiple Stop calls should not panic
	st.Stop()
	st.Stop()
	st.Stop()

	select {
	case <-st.stopChan:
		// success
	default:
		t.Fatalf("expected stopChan to be closed")
	}
}

func TestCreateGRPCWireLocal_PreservesPeerIPForUpdateWire(t *testing.T) {
	stopC := make(chan struct{})
	w := &GRPCWire{
		UID:           601,
		TopoNamespace: "default",
		LocalPodName:  "podLocal",
		LocalPodNetNS: "/proc/601/ns/net",
		PeerNodeIP:    "10.0.0.2",
		IsReady:       true,
		StopC:         stopC,
	}
	wires.AddInMem(w, nil)
	defer wires.AtomicDelete(w)

	// Call CreateGRPCWireLocal with new peer IP (peer moved to 10.0.0.3)
	resp, err := CreateGRPCWireLocal(context.Background(), &mpb.WireDef{
		LocalPodNetNs: "/proc/601/ns/net",
		LinkUid:       601,
		PeerNodeIp:    "10.0.0.3",
	})
	if err != nil || resp == nil || !resp.Response {
		t.Fatalf("CreateGRPCWireLocal failed: %v", err)
	}

	// PeerNodeIP should remain 10.0.0.2 until UpdateWire is called, ensuring stream migration occurs
	if w.PeerNodeIP != "10.0.0.2" {
		t.Fatalf("expected PeerNodeIP to remain 10.0.0.2 until UpdateWire, got %s", w.PeerNodeIP)
	}

	// Now UpdateWireByUID is called with 10.0.0.3
	updated, ok := UpdateWireByUID("/proc/601/ns/net", 601, 700, "10.0.0.3", make(chan struct{}))
	if !ok || updated == nil {
		t.Fatalf("UpdateWireByUID failed")
	}
	if updated.PeerNodeIP != "10.0.0.3" {
		t.Fatalf("expected PeerNodeIP to be updated to 10.0.0.3, got %s", updated.PeerNodeIP)
	}
}

func TestWireDownByUID_FallbackByLinkUID(t *testing.T) {
	stopC := make(chan struct{})
	w := &GRPCWire{
		UID:           707,
		TopoNamespace: "test-ns",
		LocalPodName:  "podRemote",
		LocalPodNetNS: "/proc/707/ns/net",
		IsReady:       true,
		StopC:         stopC,
	}
	wires.AddInMem(w, nil)
	defer wires.AtomicDelete(w)

	// Call WireDownByUID using TopoNamespace ("test-ns") instead of container netns ("/proc/707/ns/net")
	if err := WireDownByUID("test-ns", 707); err != nil {
		t.Fatalf("WireDownByUID failed: %v", err)
	}

	if w.IsReady {
		t.Fatalf("expected wire to be marked down (IsReady=false)")
	}

	select {
	case <-stopC:
		// success: StopC closed
	default:
		t.Fatalf("expected StopC to be closed by WireDownByUID fallback")
	}
}

func TestRelocatedPod_StreamRedirection(t *testing.T) {
	topoNs := "test-relocate"
	oldPeerIP := "192.0.2.10"
	newPeerIP := "192.0.2.20"

	stopC := make(chan struct{})
	w := &GRPCWire{
		UID:                   808,
		TopoNamespace:         topoNs,
		LocalPodName:          "podA",
		LocalPodNetNS:         "/proc/808/ns/net",
		PeerNodeIP:            oldPeerIP,
		WireIfaceIDOnPeerNode: 10,
		IsReady:               true,
		StopC:                 stopC,
	}
	wires.AddInMem(w, nil)
	defer wires.AtomicDelete(w)

	// Simulate initial stream acquisition by RecvFrmLocalPodThread
	stOld := streamMgr.GetOrCreateStream(topoNs, oldPeerIP)
	defer stOld.Stop()

	pkt := &mpb.Packet{RemotIntfId: 10, Frame: []byte{0x01, 0x02}}

	// Outbound send to old peer IP succeeds
	if !streamMgr.Send(topoNs, oldPeerIP, pkt) {
		t.Fatalf("expected Send to oldPeerIP to succeed before relocation")
	}

	// Pod B relocates to newPeerIP: UpdateWireByUID is invoked
	updated, ok := UpdateWireByUID("/proc/808/ns/net", 808, 20, newPeerIP, make(chan struct{}))
	if !ok || updated == nil {
		t.Fatalf("UpdateWireByUID failed")
	}

	// 1. PeerNodeIP is updated
	if updated.PeerNodeIP != newPeerIP {
		t.Fatalf("expected PeerNodeIP to be updated to %s, got %s", newPeerIP, updated.PeerNodeIP)
	}

	// 2. WireIfaceIDOnPeerNode is updated to new interface ID
	if updated.WireIfaceIDOnPeerNode != 20 {
		t.Fatalf("expected WireIfaceIDOnPeerNode to be 20, got %d", updated.WireIfaceIDOnPeerNode)
	}

	// 3. New stream is created and accepting packets
	if !streamMgr.Send(topoNs, newPeerIP, pkt) {
		t.Fatalf("expected Send to newPeerIP to succeed after relocation")
	}

	// 4. Old stream was released
	streamMgr.mu.Lock()
	_, oldStreamStillExists := streamMgr.streams[nodeStreamKey{topoNs: topoNs, peerIP: oldPeerIP}]
	streamMgr.mu.Unlock()

	if oldStreamStillExists {
		t.Fatalf("expected old stream %s to be released after relocation", oldPeerIP)
	}

	// Clean up new stream
	streamMgr.ReleaseStream(topoNs, newPeerIP)
}

func TestPassivePodRestart_SymmetricRecovery(t *testing.T) {
	// Node 1 hosts Active Pod A (ID 50)
	stopC := make(chan struct{})
	wireA := &GRPCWire{
		UID:                   909,
		TopoNamespace:         "default",
		LocalNodeIfaceID:      50,
		LocalPodName:          "podActive",
		LocalPodNetNS:         "/proc/podA/ns/net",
		PeerNodeIP:            "10.0.0.2",
		WireIfaceIDOnPeerNode: 100, // Old ID on Node 2
		IsReady:               true,
		StopC:                 stopC,
		Originator:            HOST_CREATED_WIRE,
	}
	wires.AddInMem(wireA, nil)
	defer wires.AtomicDelete(wireA)

	// Passive Pod B on Node 2 restarts and gets new ID 200.
	// Node 2 initiates connection to Node 1 via CreateUpdateGRPCWireRemoteTriggered
	wireDefFromPassive := &mpb.WireDef{
		LocalPodNetNs:      "/proc/podA/ns/net",
		LinkUid:            909,
		WireIfIdOnPeerNode: 200, // New ID on Node 2
		PeerNodeIp:         "10.0.0.2",
		TopoNs:             "default",
		LocalPodName:       "podActive",
	}

	wire, created, err := CreateUpdateGRPCWireRemoteTriggered(wireDefFromPassive, make(chan struct{}))
	if err != nil {
		t.Fatalf("CreateUpdateGRPCWireRemoteTriggered failed: %v", err)
	}

	if created {
		t.Fatalf("expected created=false for existing wire on active pod")
	}

	if wire.LocalNodeIfaceID != 50 {
		t.Fatalf("expected LocalNodeIfaceID=50, got %d", wire.LocalNodeIfaceID)
	}

	if wire.WireIfaceIDOnPeerNode != 200 {
		t.Fatalf("expected WireIfaceIDOnPeerNode to be updated to 200, got %d", wire.WireIfaceIDOnPeerNode)
	}

	if !wire.IsReady {
		t.Fatalf("expected wire to remain ready")
	}
}

func TestWireDownByUID_CleansUpAndRemovesFromMemory(t *testing.T) {
	stopC := make(chan struct{})
	w := &GRPCWire{
		UID:           950,
		TopoNamespace: "default",
		LocalPodName:  "podDownTest",
		LocalPodNetNS: "/proc/950/ns/net",
		PeerNodeIP:    "10.0.0.2",
		IsReady:       true,
		StopC:         stopC,
	}
	wires.AddInMem(w, nil)

	// WireDownByUID should cleanly remove wire from memory and close StopC
	if err := WireDownByUID("/proc/950/ns/net", 950); err != nil {
		t.Fatalf("WireDownByUID failed: %v", err)
	}

	// 1. Verify StopC closed
	select {
	case <-stopC:
	default:
		t.Fatalf("expected stopC to be closed")
	}

	// 2. Verify wire is removed from in-memory active map
	if _, exists := GetWireByUID("/proc/950/ns/net", 950); exists {
		t.Fatalf("expected wire to be removed from in-memory map by WireDownByUID")
	}
}

func TestGRPCWireDownRemoteTriggered_RemoteNetNSFallback(t *testing.T) {
	stopC := make(chan struct{})
	w := &GRPCWire{
		UID:           960,
		TopoNamespace: "test-topo-ns",
		LocalPodName:  "podLocal",
		LocalPodNetNS: "/proc/nodeB/local/ns/net",
		PeerNodeIP:    "10.0.0.1",
		IsReady:       true,
		StopC:         stopC,
	}
	wires.AddInMem(w, nil)
	defer wires.AtomicDelete(w)

	// Node A sends GRPCWireDownRemoteTriggered with Node A's netns (/proc/nodeA/remote/ns/net) and TopoNs
	wireDefFromRemote := &mpb.WireDef{
		LocalPodNetNs: "/proc/nodeA/remote/ns/net",
		LinkUid:       960,
		TopoNs:        "test-topo-ns",
		LocalPodName:  "podRemote",
	}

	if err := GRPCWireDownRemoteTriggered(wireDefFromRemote); err != nil {
		t.Fatalf("GRPCWireDownRemoteTriggered failed: %v", err)
	}

	// Wire should be marked down and removed
	if w.IsReady {
		t.Fatalf("expected wire to be marked down")
	}

	select {
	case <-stopC:
		// success: stopC was closed
	default:
		t.Fatalf("expected stopC to be closed")
	}

	if _, exists := GetWireByUID("/proc/nodeB/local/ns/net", 960); exists {
		t.Fatalf("expected wire to be removed from memory by GRPCWireDownRemoteTriggered")
	}
}

