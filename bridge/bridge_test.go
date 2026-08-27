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
		return nil, fmt.Errorf("fake socket closed")
	}
	return pkt, nil
}

func (f *fakeReadWriter) WritePacket(pkt []byte) error {
	f.mu.Lock()
	defer f.mu.Unlock()
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

	sub1 := demux.subscribe()
	sub2 := demux.subscribe()

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

	sub := demux.subscribe()

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
