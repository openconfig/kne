package grpcwire

import (
	"bytes"
	"context"
	"errors"
	"io"
	"sync"
	"testing"
	"time"

	mpb "github.com/openconfig/kne/third_party/meshnet/daemon/proto/meshnet/v1beta1"
	"google.golang.org/grpc"
)

type mockWireProtocolClient struct {
	mpb.UnimplementedWireProtocolServer
	mu             sync.Mutex
	receivedFrames [][]byte
	sendDelay      time.Duration
}

func (m *mockWireProtocolClient) SendToOnce(ctx context.Context, in *mpb.Packet, opts ...grpc.CallOption) (*mpb.BoolResponse, error) {
	if m.sendDelay > 0 {
		time.Sleep(m.sendDelay)
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	// Copy frame to verify exact payload received
	frameCopy := make([]byte, len(in.Frame))
	copy(frameCopy, in.Frame)
	m.receivedFrames = append(m.receivedFrames, frameCopy)
	return &mpb.BoolResponse{Response: true}, nil
}

func (m *mockWireProtocolClient) SendToStream(ctx context.Context, opts ...grpc.CallOption) (grpc.ClientStreamingClient[mpb.Packet, mpb.BoolResponse], error) {
	return nil, errors.New("unimplemented")
}

func TestForwardPackets_NoCorruption(t *testing.T) {
	InitLogger()

	pr, pw := io.Pipe()
	defer pr.Close()

	mockClient := &mockWireProtocolClient{
		// Simulate network latency so reader goroutine reads next packet while SendToOnce is busy
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
		errCh <- forwardPackets(context.Background(), pr, mockClient, wire, "eth1-0001")
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

	mockClient.mu.Lock()
	received := mockClient.receivedFrames
	mockClient.mu.Unlock()

	if len(received) != numPackets {
		t.Fatalf("expected %d packets, got %d", numPackets, len(received))
	}

	for i, exp := range expectedPackets {
		if !bytes.Equal(received[i], exp) {
			t.Errorf("packet %d corrupted: expected all 0x%02x, got mismatch", i, exp[0])
		}
	}
}
