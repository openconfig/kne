package grpcwire

import (
	"bytes"
	"errors"
	"io"
	"os"
	"sync"
	"testing"
	"time"

	mpb "github.com/openconfig/kne/third_party/meshnet/daemon/proto/meshnet/v1beta1"
)

type mockPacketSender struct {
	mu             sync.Mutex
	receivedFrames [][]byte
	sendDelay      time.Duration
}

func (m *mockPacketSender) Send(pkt *mpb.Packet) bool {
	if m.sendDelay > 0 {
		time.Sleep(m.sendDelay)
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	// Copy frame to verify exact payload received
	frameCopy := make([]byte, len(pkt.Frame))
	copy(frameCopy, pkt.Frame)
	m.receivedFrames = append(m.receivedFrames, frameCopy)
	return true
}

func TestForwardPackets_NoCorruption(t *testing.T) {
	InitLogger()

	pr, pw := io.Pipe()
	defer pr.Close()

	mockSender := &mockPacketSender{
		// Simulate network latency so reader goroutine reads next packet while Send is busy
		sendDelay: 5 * time.Millisecond,
	}

	stopC := make(chan struct{})
	wireDef := &mpb.WireDef{
		LinkUid:       1,
		IntfNameInPod: "eth1",
		LocalPodName:  "podA",
		LocalPodNetNs: "nsA",
		PeerNodeIp:    "1.2.3.4",
	}
	wire := CreateGWire(1, "eth1-0001", stopC, wireDef)
	wire.WireIfaceIDOnPeerNode = 42
	wire.IsReady = true

	errCh := make(chan error, 1)
	go func() {
		errCh <- forwardPackets(pr, mockSender, wire, "eth1-0001")
	}()

	numPackets := 50
	expectedPackets := make([][]byte, numPackets)
	for i := 0; i < numPackets; i++ {
		payload := bytes.Repeat([]byte{byte(i + 1)}, 1024)
		expectedPackets[i] = payload
		if _, err := pw.Write(payload); err != nil {
			t.Fatalf("failed to write packet %d: %v", i, err)
		}
	}

	// Allow all packets to be transmitted
	time.Sleep(350 * time.Millisecond)

	// Stop wire
	close(stopC)
	_ = pw.Close()

	err := <-errCh
	if err != nil && !errors.Is(err, io.EOF) {
		t.Fatalf("forwardPackets failed with unexpected error: %v", err)
	}

	mockSender.mu.Lock()
	received := mockSender.receivedFrames
	mockSender.mu.Unlock()

	if len(received) != numPackets {
		t.Fatalf("expected %d packets, got %d", numPackets, len(received))
	}

	for i, exp := range expectedPackets {
		if !bytes.Equal(received[i], exp) {
			t.Errorf("packet %d corrupted: expected all 0x%02x, got mismatch", i, exp[0])
		}
	}
}

func TestForwardPackets_ReaderTeardownNoLeak(t *testing.T) {
	InitLogger()

	pr, pw := io.Pipe()
	defer pr.Close()
	defer pw.Close()

	mockSender := &mockPacketSender{}
	stopC := make(chan struct{})
	wireDef := &mpb.WireDef{
		LinkUid:       2,
		IntfNameInPod: "eth1",
		LocalPodName:  "podB",
		LocalPodNetNs: "nsB",
		PeerNodeIp:    "1.2.3.5",
	}
	wire := CreateGWire(2, "eth1-0002", stopC, wireDef)
	wire.WireIfaceIDOnPeerNode = 43
	wire.IsReady = true

	doneCh := make(chan error, 1)
	go func() {
		doneCh <- forwardPackets(pr, mockSender, wire, "eth1-0002")
	}()

	// Write one packet so readChan has an entry
	if _, err := pw.Write([]byte("hello world")); err != nil {
		t.Fatalf("failed to write payload: %v", err)
	}

	time.Sleep(50 * time.Millisecond)

	// Close stopC to trigger teardown while reader might be blocked on next read
	close(stopC)

	select {
	case err := <-doneCh:
		if err != nil && !errors.Is(err, io.EOF) {
			t.Fatalf("unexpected error on teardown: %v", err)
		}
	case <-time.After(1 * time.Second):
		t.Fatalf("forwardPackets did not terminate within timeout upon stopC close")
	}
}

func TestWireMap_HandleCleanup(t *testing.T) {
	stopC := make(chan struct{})
	wireDef := &mpb.WireDef{
		LinkUid:       3,
		IntfNameInPod: "eth3",
		LocalPodName:  "podC",
		LocalPodNetNs: "nsC",
		TopoNs:        "topoC",
		PeerNodeIp:    "1.2.3.6",
	}
	wire := CreateGWire(10, "eth3-0010", stopC, wireDef)

	r, w, err := os.Pipe()
	if err != nil {
		t.Fatalf("failed to create pipe: %v", err)
	}
	defer w.Close()

	_ = wires.AddInMem(wire, r)

	// Verify handle is present
	if _, ok := wires.GetHandle(10); !ok {
		t.Fatalf("expected handle 10 in map")
	}

	// Extract wire
	extracted, ok := ExtractOneWireByPod("topoC", "podC")
	if !ok || extracted == nil {
		t.Fatalf("expected to extract wire for podC")
	}

	// Handle should still exist in map until explicitly closed/removed by RemoveWireAcrosAll
	if _, ok := wires.GetHandle(10); !ok {
		t.Fatalf("expected handle 10 to remain until CloseAndRemoveHandle")
	}

	// Close and remove handle
	if err := wires.CloseAndRemoveHandle(10); err != nil {
		t.Fatalf("failed to close and remove handle: %v", err)
	}

	if _, ok := wires.GetHandle(10); ok {
		t.Fatalf("expected handle 10 to be removed from map")
	}
}

func TestCreateUpdateGRPCWireRemoteTriggered_CreatedFlag(t *testing.T) {
	wireDef := &mpb.WireDef{
		LinkUid:            100,
		IntfNameInPod:      "eth100",
		LocalPodName:       "pod100",
		LocalPodNetNs:      "netns100",
		TopoNs:             "topo100",
		WireIfIdOnPeerNode: 50,
		PeerNodeIp:         "192.168.10.10",
	}

	// First create locally
	wireID := NextIndex()
	localWire := CreateGWire(int(wireID), wireDef.IntfNameInPod, make(chan struct{}), wireDef)
	localWire.IsReady = false
	_ = wires.AddInMem(localWire, nil)

	// Now remote trigger should update existing wire, not create fresh
	stopC := make(chan struct{})
	wire, created, err := CreateUpdateGRPCWireRemoteTriggered(wireDef, stopC)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if created {
		t.Fatalf("expected created == false for existing wire update")
	}
	if wire.WireIfaceIDOnPeerNode != 50 {
		t.Fatalf("expected PeerIntfId 50, got %d", wire.WireIfaceIDOnPeerNode)
	}
}
