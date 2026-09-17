// Copyright 2026 Google LLC
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//      http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package bridge

import (
	"bytes"
	"context"
	"fmt"
	"net"
	"sync"
	"testing"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/test/bufconn"

	"golang.org/x/sys/unix"
	wpb "github.com/openconfig/kne/proto/wire"
)

type fakeReadWriter struct {
	mu        sync.Mutex
	readChan  chan []byte
	writeChan chan []byte
	writeErr  error
	closed    bool
}

func newFakeReadWriter() *fakeReadWriter {
	return &fakeReadWriter{
		readChan:  make(chan []byte, 100),
		writeChan: make(chan []byte, 100),
	}
}

func (f *fakeReadWriter) ReadPacket() ([]byte, error) {
	pkt, ok := <-f.readChan
	if !ok {
		return nil, net.ErrClosed
	}
	return pkt, nil
}

func (f *fakeReadWriter) WritePacket(pkt []byte) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.closed {
		return net.ErrClosed
	}
	if f.writeErr != nil {
		return f.writeErr
	}
	f.writeChan <- pkt
	return nil
}

func (f *fakeReadWriter) Close() error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if !f.closed {
		f.closed = true
		close(f.readChan)
	}
	return nil
}

func (f *fakeReadWriter) isClosed() bool {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.closed
}

func TestTransmitBidirectionalStream(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	server := NewServer(ctx)
	defer func() {
		_ = server.Close()
	}()

	fakeIO := newFakeReadWriter()
	server.SetSocketOpener(func(ifaceName string) (ReadWriter, error) {
		return fakeIO, nil
	})

	lis := bufconn.Listen(1024 * 1024)
	grpcServer := grpc.NewServer()
	wpb.RegisterWireServer(grpcServer, server)

	go func() {
		_ = grpcServer.Serve(lis)
	}()
	defer grpcServer.Stop()

	conn, err := grpc.NewClient("passthrough://bufnet",
		grpc.WithContextDialer(func(context.Context, string) (net.Conn, error) {
			return lis.Dial()
		}),
		grpc.WithTransportCredentials(insecure.NewCredentials()),
	)
	if err != nil {
		t.Fatalf("Failed to dial bufnet: %v", err)
	}
	defer func() {
		_ = conn.Close()
	}()

	client := wpb.NewWireClient(conn)
	streamCtx := metadata.NewOutgoingContext(ctx, metadata.Pairs("interface", "eth1"))
	stream, err := client.Transmit(streamCtx)
	if err != nil {
		t.Fatalf("Transmit RPC failed: %v", err)
	}
	if _, err := stream.Header(); err != nil {
		t.Fatalf("Failed to receive stream header: %v", err)
	}

	// 1. Test Egress (Raw Socket -> gRPC Client)
	egressPacket := []byte{0x01, 0x02, 0x03, 0x04}
	fakeIO.readChan <- egressPacket

	resp, err := stream.Recv()
	if err != nil {
		t.Fatalf("Failed to receive packet from stream: %v", err)
	}
	if !bytes.Equal(resp.GetData(), egressPacket) {
		t.Fatalf("Egress packet mismatch: got %v, want %v", resp.GetData(), egressPacket)
	}

	// 2. Test Ingress (gRPC Client -> Raw Socket)
	ingressPacket := []byte{0x05, 0x06, 0x07, 0x08}
	if err := stream.Send(&wpb.Packet{Data: ingressPacket}); err != nil {
		t.Fatalf("Failed to send packet to stream: %v", err)
	}

	select {
	case receivedPkt := <-fakeIO.writeChan:
		if !bytes.Equal(receivedPkt, ingressPacket) {
			t.Fatalf("Ingress packet mismatch: got %v, want %v", receivedPkt, ingressPacket)
		}
	case <-time.After(1 * time.Second):
		t.Fatalf("Timed out waiting for ingress packet to be written to raw socket")
	}
}

func TestDemuxerSlowSubscriberDoesNotDeadlock(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	fakeIO := newFakeReadWriter()
	demux := newInterfaceDemux(ctx, "eth1", fakeIO, nil)

	sub1, err := demux.subscribe()
	if err != nil {
		t.Fatalf("sub1 subscribe failed: %v", err)
	}
	sub2, err := demux.subscribe()
	if err != nil {
		t.Fatalf("sub2 subscribe failed: %v", err)
	}

	// Fill sub1 channel to capacity
	for i := 0; i < channelBufferCap; i++ {
		sub1 <- []byte{byte(i)}
	}

	// Send new packet through raw socket
	testPkt := []byte{0xAA, 0xBB, 0xCC}
	fakeIO.readChan <- testPkt

	// sub2 should receive testPkt without blocking
	select {
	case pkt := <-sub2:
		if !bytes.Equal(pkt, testPkt) {
			t.Fatalf("sub2 packet mismatch: got %v, want %v", pkt, testPkt)
		}
	case <-time.After(1 * time.Second):
		t.Fatalf("sub2 timed out waiting for packet (demuxer deadlocked)")
	}

	// Dropped frame counter on demux should be at least 1 for sub1
	if demux.droppedFrames.Load() == 0 {
		t.Fatalf("Expected droppedFrames > 0 for full subscriber channel")
	}

	// Calling unsubscribe on the full channel must not deadlock
	done := make(chan struct{})
	go func() {
		demux.unsubscribe(sub1)
		close(done)
	}()

	select {
	case <-done:
		// Success: unsubscribe did not deadlock with readLoop
	case <-time.After(1 * time.Second):
		t.Fatalf("unsubscribe deadlocked waiting for RLock")
	}
}

func TestTransmitMissingInterfaceMetadata(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	server := NewServer(ctx)
	defer func() {
		_ = server.Close()
	}()

	lis := bufconn.Listen(1024 * 1024)
	grpcServer := grpc.NewServer()
	wpb.RegisterWireServer(grpcServer, server)

	go func() {
		_ = grpcServer.Serve(lis)
	}()
	defer grpcServer.Stop()

	conn, err := grpc.NewClient("passthrough://bufnet",
		grpc.WithContextDialer(func(context.Context, string) (net.Conn, error) {
			return lis.Dial()
		}),
		grpc.WithTransportCredentials(insecure.NewCredentials()),
	)
	if err != nil {
		t.Fatalf("Failed to dial bufnet: %v", err)
	}
	defer func() {
		_ = conn.Close()
	}()

	client := wpb.NewWireClient(conn)

	// Case 1: No metadata header attached
	stream1, err := client.Transmit(ctx)
	if err != nil {
		t.Fatalf("Transmit RPC creation failed: %v", err)
	}
	_, err = stream1.Recv()
	if err == nil {
		t.Fatalf("Expected error when metadata is missing, got nil")
	}

	// Case 2: Metadata attached but 'interface' key is missing
	streamCtx := metadata.NewOutgoingContext(ctx, metadata.Pairs("other_key", "val"))
	stream2, err := client.Transmit(streamCtx)
	if err != nil {
		t.Fatalf("Transmit RPC creation failed: %v", err)
	}
	_, err = stream2.Recv()
	if err == nil {
		t.Fatalf("Expected error when 'interface' header is missing, got nil")
	}
}

func TestTransmitSocketOpenerError(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	server := NewServer(ctx)
	defer func() {
		_ = server.Close()
	}()

	server.SetSocketOpener(func(ifaceName string) (ReadWriter, error) {
		return nil, fmt.Errorf("interface %s does not exist", ifaceName)
	})

	lis := bufconn.Listen(1024 * 1024)
	grpcServer := grpc.NewServer()
	wpb.RegisterWireServer(grpcServer, server)

	go func() {
		_ = grpcServer.Serve(lis)
	}()
	defer grpcServer.Stop()

	conn, err := grpc.NewClient("passthrough://bufnet",
		grpc.WithContextDialer(func(context.Context, string) (net.Conn, error) {
			return lis.Dial()
		}),
		grpc.WithTransportCredentials(insecure.NewCredentials()),
	)
	if err != nil {
		t.Fatalf("Failed to dial bufnet: %v", err)
	}
	defer func() {
		_ = conn.Close()
	}()

	client := wpb.NewWireClient(conn)
	streamCtx := metadata.NewOutgoingContext(ctx, metadata.Pairs("interface", "nonexistent0"))
	stream, err := client.Transmit(streamCtx)
	if err != nil {
		t.Fatalf("Transmit RPC creation failed: %v", err)
	}

	_, err = stream.Recv()
	if err == nil {
		t.Fatalf("Expected error when socketOpener fails, got nil")
	}
}

func TestServerCloseToTeardown(t *testing.T) {
	ctx := context.Background()
	server := NewServer(ctx)

	fakeIO := newFakeReadWriter()
	server.SetSocketOpener(func(ifaceName string) (ReadWriter, error) {
		return fakeIO, nil
	})

	demux, err := server.getOrCreateDemux("eth1")
	if err != nil {
		t.Fatalf("getOrCreateDemux failed: %v", err)
	}

	sub, err := demux.subscribe()
	if err != nil {
		t.Fatalf("subscribe failed: %v", err)
	}

	// Closing server should cancel parent context, closing subscriber channels and socket handler
	_ = server.Close()

	select {
	case _, ok := <-sub:
		if ok {
			t.Fatalf("Expected subscriber channel to be closed on server close")
		}
	case <-time.After(1 * time.Second):
		t.Fatalf("Timed out waiting for subscriber channel to close on server close")
	}

	if !fakeIO.isClosed() {
		t.Fatalf("Expected fakeIO handler to be closed on server close")
	}
}

func TestSubscribeOnClosedDemuxFails(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	fakeIO := newFakeReadWriter()
	demux := newInterfaceDemux(ctx, "eth1", fakeIO, nil)

	// Close fakeIO which terminates readLoop and marks demux closed
	_ = fakeIO.Close()
	_ = demux.wait()

	_, err := demux.subscribe()
	if err == nil {
		t.Fatalf("Expected subscribe() on dead demux to return an error, got nil")
	}
}

func TestDemuxMaxSubscribers(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	fakeIO := newFakeReadWriter()
	demux := newInterfaceDemux(ctx, "eth1", fakeIO, nil)

	var subs []chan []byte
	for i := 0; i < maxSubscribersPerDemux; i++ {
		sub, err := demux.subscribe()
		if err != nil {
			t.Fatalf("Failed subscribing at index %d: %v", i, err)
		}
		subs = append(subs, sub)
	}

	// 33rd subscriber must fail
	_, err := demux.subscribe()
	if err == nil {
		t.Fatalf("Expected error when exceeding maxSubscribersPerDemux, got nil")
	}

	// Unsubscribe one and then subscribe should succeed
	demux.unsubscribe(subs[0])
	_, err = demux.subscribe()
	if err != nil {
		t.Fatalf("Expected subscribe to succeed after unsubscribing, got: %v", err)
	}
}

func TestInterfaceDemuxWriteCheckContext(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	fakeIO := newFakeReadWriter()
	demux := newInterfaceDemux(ctx, "eth1", fakeIO, nil)

	cancel()
	err := demux.write([]byte{0x01, 0x02})
	if err != net.ErrClosed {
		t.Fatalf("Expected net.ErrClosed after context cancel, got: %v", err)
	}
}

func TestGetOrCreateDemuxPurgesDeadDemux(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	server := NewServer(ctx)
	fakeIO1 := newFakeReadWriter()
	fakeIO2 := newFakeReadWriter()

	var openerCount int
	var openerMu sync.Mutex
	server.SetSocketOpener(func(ifaceName string) (ReadWriter, error) {
		openerMu.Lock()
		defer openerMu.Unlock()
		openerCount++
		if openerCount == 1 {
			return fakeIO1, nil
		}
		return fakeIO2, nil
	})

	d1, err := server.getOrCreateDemux("eth1")
	if err != nil {
		t.Fatalf("First getOrCreateDemux failed: %v", err)
	}

	// Kill first demux
	_ = fakeIO1.Close()
	_ = d1.wait()

	// getOrCreateDemux should detect dead d1 and return fresh d2
	d2, err := server.getOrCreateDemux("eth1")
	if err != nil {
		t.Fatalf("Second getOrCreateDemux failed: %v", err)
	}
	if d1 == d2 {
		t.Fatalf("Expected getOrCreateDemux to return a new demux instance, got the dead one")
	}
}

func TestGetOrCreateDemuxAfterServerClose(t *testing.T) {
	ctx := context.Background()
	server := NewServer(ctx)
	_ = server.Close()

	_, err := server.getOrCreateDemux("eth1")
	if err == nil {
		t.Fatalf("Expected error when calling getOrCreateDemux after server.Close(), got nil")
	}
}

func TestServerCloseWaitsForReadLoops(t *testing.T) {
	ctx := context.Background()
	server := NewServer(ctx)

	fakeIO := newFakeReadWriter()
	server.SetSocketOpener(func(ifaceName string) (ReadWriter, error) {
		return fakeIO, nil
	})

	_, err := server.getOrCreateDemux("eth1")
	if err != nil {
		t.Fatalf("getOrCreateDemux failed: %v", err)
	}

	if err := server.Close(); err != nil {
		t.Fatalf("server.Close() returned error: %v", err)
	}

	if !fakeIO.isClosed() {
		t.Fatalf("Expected fakeIO to be closed after server.Close() returns")
	}
}

func TestIngressNonFatalErrorContinues(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	server := NewServer(ctx)
	defer func() { _ = server.Close() }()

	fakeIO := newFakeReadWriter()
	fakeIO.writeErr = fmt.Errorf("message too long: %w", unix.EMSGSIZE)
	server.SetSocketOpener(func(ifaceName string) (ReadWriter, error) {
		return fakeIO, nil
	})

	lis := bufconn.Listen(1024 * 1024)
	grpcServer := grpc.NewServer()
	wpb.RegisterWireServer(grpcServer, server)

	go func() {
		_ = grpcServer.Serve(lis)
	}()
	defer grpcServer.Stop()

	conn, err := grpc.NewClient("passthrough://bufnet",
		grpc.WithContextDialer(func(context.Context, string) (net.Conn, error) {
			return lis.Dial()
		}),
		grpc.WithTransportCredentials(insecure.NewCredentials()),
	)
	if err != nil {
		t.Fatalf("Failed to dial bufnet: %v", err)
	}
	defer func() { _ = conn.Close() }()

	client := wpb.NewWireClient(conn)
	streamCtx := metadata.NewOutgoingContext(ctx, metadata.Pairs("interface", "eth1"))
	stream, err := client.Transmit(streamCtx)
	if err != nil {
		t.Fatalf("Transmit RPC failed: %v", err)
	}
	if _, err := stream.Header(); err != nil {
		t.Fatalf("Failed to receive stream header: %v", err)
	}

	// Send non-fatal bad packet: should not tear down stream
	if err := stream.Send(&wpb.Packet{Data: []byte{0x01, 0x02}}); err != nil {
		t.Fatalf("Send packet failed: %v", err)
	}

	// Clear write error and send valid packet
	time.Sleep(50 * time.Millisecond)
	fakeIO.mu.Lock()
	fakeIO.writeErr = nil
	fakeIO.mu.Unlock()

	validPkt := []byte{0xAA, 0xBB}
	if err := stream.Send(&wpb.Packet{Data: validPkt}); err != nil {
		t.Fatalf("Send second packet failed: %v", err)
	}

	select {
	case received := <-fakeIO.writeChan:
		if !bytes.Equal(received, validPkt) {
			t.Fatalf("Packet mismatch: got %v, want %v", received, validPkt)
		}
	case <-time.After(1 * time.Second):
		t.Fatalf("Timed out waiting for valid packet on fakeIO")
	}
}

func TestFatalWriteErrorDetection(t *testing.T) {
	if !isFatalWriteError(net.ErrClosed) {
		t.Errorf("expected net.ErrClosed to be fatal")
	}
	if !isFatalWriteError(unix.EBADF) {
		t.Errorf("expected unix.EBADF to be fatal")
	}
	if !isFatalWriteError(unix.ENETDOWN) {
		t.Errorf("expected unix.ENETDOWN to be fatal")
	}
	if isFatalWriteError(unix.EMSGSIZE) {
		t.Errorf("expected unix.EMSGSIZE to be non-fatal")
	}
	if isFatalWriteError(unix.ENOBUFS) {
		t.Errorf("expected unix.ENOBUFS to be non-fatal")
	}
}
