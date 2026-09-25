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
	"encoding/binary"
	"fmt"
	"net"
	"sync"
	"testing"
	"time"

	"github.com/safchain/ethtool"
	"golang.org/x/sys/unix"
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

func (f *fakeReadWriter) sendPacket(pkt []byte) bool {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.closed {
		return false
	}
	f.readChan <- pkt
	return true
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

func TestTransmitMultiClientFanOut(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	server := NewServer(ctx)
	defer func() { _ = server.Close() }()

	fakeIO := newFakeReadWriter()
	server.SetSocketOpener(func(ifaceName string) (ReadWriter, error) {
		return fakeIO, nil
	})

	lis := bufconn.Listen(1024 * 1024)
	grpcServer := grpc.NewServer()
	wpb.RegisterWireServer(grpcServer, server)

	go func() { _ = grpcServer.Serve(lis) }()
	defer grpcServer.Stop()

	dialer := func(context.Context, string) (net.Conn, error) { return lis.Dial() }
	conn1, err := grpc.NewClient("passthrough://bufnet", grpc.WithContextDialer(dialer), grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		t.Fatalf("Failed to dial conn1: %v", err)
	}
	defer func() { _ = conn1.Close() }()

	conn2, err := grpc.NewClient("passthrough://bufnet", grpc.WithContextDialer(dialer), grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		t.Fatalf("Failed to dial conn2: %v", err)
	}
	defer func() { _ = conn2.Close() }()

	c1 := wpb.NewWireClient(conn1)
	c2 := wpb.NewWireClient(conn2)

	streamCtx1 := metadata.NewOutgoingContext(ctx, metadata.Pairs("interface", "eth1"))
	s1, err := c1.Transmit(streamCtx1)
	if err != nil {
		t.Fatalf("s1 Transmit failed: %v", err)
	}
	if _, err := s1.Header(); err != nil {
		t.Fatalf("s1 header failed: %v", err)
	}

	streamCtx2 := metadata.NewOutgoingContext(ctx, metadata.Pairs("interface", "eth1"))
	s2, err := c2.Transmit(streamCtx2)
	if err != nil {
		t.Fatalf("s2 Transmit failed: %v", err)
	}
	if _, err := s2.Header(); err != nil {
		t.Fatalf("s2 header failed: %v", err)
	}

	// Send broadcast frame from fake socket
	broadcastPkt := []byte{0xDE, 0xAD, 0xBE, 0xEF}
	fakeIO.readChan <- broadcastPkt

	// Both clients must receive the exact same frame
	r1, err := s1.Recv()
	if err != nil {
		t.Fatalf("s1 Recv failed: %v", err)
	}
	if !bytes.Equal(r1.GetData(), broadcastPkt) {
		t.Fatalf("s1 got %v, want %v", r1.GetData(), broadcastPkt)
	}

	r2, err := s2.Recv()
	if err != nil {
		t.Fatalf("s2 Recv failed: %v", err)
	}
	if !bytes.Equal(r2.GetData(), broadcastPkt) {
		t.Fatalf("s2 got %v, want %v", r2.GetData(), broadcastPkt)
	}
}

func TestSocketOpenerInvokedOncePerInterface(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	server := NewServer(ctx)
	defer func() { _ = server.Close() }()

	var mu sync.Mutex
	openerCounts := map[string]int{}
	fakes := map[string]*fakeReadWriter{}

	server.SetSocketOpener(func(ifaceName string) (ReadWriter, error) {
		mu.Lock()
		defer mu.Unlock()
		openerCounts[ifaceName]++
		f := newFakeReadWriter()
		fakes[ifaceName] = f
		return f, nil
	})

	lis := bufconn.Listen(1024 * 1024)
	grpcServer := grpc.NewServer()
	wpb.RegisterWireServer(grpcServer, server)

	go func() { _ = grpcServer.Serve(lis) }()
	defer grpcServer.Stop()

	dialer := func(context.Context, string) (net.Conn, error) { return lis.Dial() }
	conn, err := grpc.NewClient("passthrough://bufnet", grpc.WithContextDialer(dialer), grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		t.Fatalf("dial failed: %v", err)
	}
	defer func() { _ = conn.Close() }()

	client := wpb.NewWireClient(conn)

	// Stream 1 on eth1
	s1, err := client.Transmit(metadata.NewOutgoingContext(ctx, metadata.Pairs("interface", "eth1")))
	if err != nil {
		t.Fatalf("s1 failed: %v", err)
	}
	_, _ = s1.Header()

	// Stream 2 on eth1
	s2, err := client.Transmit(metadata.NewOutgoingContext(ctx, metadata.Pairs("interface", "eth1")))
	if err != nil {
		t.Fatalf("s2 failed: %v", err)
	}
	_, _ = s2.Header()

	// Stream 3 on eth2
	s3, err := client.Transmit(metadata.NewOutgoingContext(ctx, metadata.Pairs("interface", "eth2")))
	if err != nil {
		t.Fatalf("s3 failed: %v", err)
	}
	_, _ = s3.Header()

	mu.Lock()
	eth1Count := openerCounts["eth1"]
	eth2Count := openerCounts["eth2"]
	mu.Unlock()

	if eth1Count != 1 {
		t.Errorf("expected eth1 socket opener called 1 time, got %d", eth1Count)
	}
	if eth2Count != 1 {
		t.Errorf("expected eth2 socket opener called 1 time, got %d", eth2Count)
	}
}

func TestOnCloseCacheEvictionAndDeadDemuxSubscribe(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	fake := newFakeReadWriter()
	d := newInterfaceDemux(ctx, "eth1", fake, nil)

	_ = fake.Close()
	<-d.ctx.Done()
	time.Sleep(50 * time.Millisecond)

	if _, err := d.subscribe(); err == nil {
		t.Error("subscribe on a dead demux must fail")
	}
}

func TestDemuxConcurrentFanOutStress(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())

	fakeIO := newFakeReadWriter()
	demux := newInterfaceDemux(ctx, "eth1", fakeIO, nil)

	var wg sync.WaitGroup
	const numSubscribers = 10
	const numPackets = 200

	// Producer pumping packets
	wg.Add(1)
	go func() {
		defer wg.Done()
		for i := 0; i < numPackets; i++ {
			select {
			case <-ctx.Done():
				return
			default:
				if !fakeIO.sendPacket([]byte{byte(i)}) {
					return
				}
			}
		}
	}()

	// Concurrent subscribers subscribing, reading, unsubscribing
	for i := 0; i < numSubscribers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < 10; j++ {
				ch, err := demux.subscribe()
				if err != nil {
					return
				}
				// Read a few packets
				for k := 0; k < 3; k++ {
					select {
					case _, ok := <-ch:
						if !ok {
							return
						}
					case <-time.After(5 * time.Millisecond):
					case <-ctx.Done():
						demux.unsubscribe(ch)
						return
					}
				}
				demux.unsubscribe(ch)
			}
		}()
	}

	time.Sleep(50 * time.Millisecond)
	cancel()
	_ = fakeIO.Close()
	wg.Wait()
	_ = demux.wait()
}

func TestOffloadDisabledMapIncludesRequiredFeatures(t *testing.T) {
	required := []string{
		"tx-checksum-ipv4",
		"tx-checksum-ipv6",
		"tx-checksum-ip-generic",
		"tx-tcp-segmentation",
		"tx-tcp6-segmentation",
		"tx-checksum-fcoe-crc",
		"tx-checksum-sctp",
		"tx-tcp-ecn-segmentation",
		"tx-tcp-mangleid-segmentation",
		"tx-generic-segmentation",
		"tx-udp-segmentation",
		"rx-gro",
		"rx-lro",
		"rx-checksum",
	}
	for _, feat := range required {
		val, ok := offloadDisabledMap[feat]
		if !ok {
			t.Errorf("offloadDisabledMap missing required feature %q", feat)
		} else if val {
			t.Errorf("offloadDisabledMap[%q] = true, want false", feat)
		}
	}
}

type fakeEthtool struct {
	states    map[string]ethtool.FeatureState
	statesErr error
	changeErr error
	changed   map[string]bool
	closed    bool
}

func (f *fakeEthtool) FeaturesWithState(_ string) (map[string]ethtool.FeatureState, error) {
	if f.statesErr != nil {
		return nil, f.statesErr
	}
	return f.states, nil
}

func (f *fakeEthtool) Change(_ string, config map[string]bool) error {
	f.changed = make(map[string]bool, len(config))
	for k, v := range config {
		f.changed[k] = v
	}
	return f.changeErr
}

func (f *fakeEthtool) Close() {
	f.closed = true
}

func TestDisableHardwareOffloads(t *testing.T) {
	origNewEthtool := newEthtool
	t.Cleanup(func() {
		newEthtool = origNewEthtool
	})

	t.Run("disables only active changeable features", func(t *testing.T) {
		fe := &fakeEthtool{
			states: map[string]ethtool.FeatureState{
				"tx-checksum-ip-generic": {Available: true, Active: true, NeverChanged: false},
				"tx-checksum-ipv4":       {Available: false, Active: false, NeverChanged: true},
				"rx-gro":                 {Available: true, Active: false, NeverChanged: false},
				"rx-checksum":            {Available: true, Active: true, NeverChanged: true},
			},
		}
		newEthtool = func() (ethtoolClient, error) { return fe, nil }

		if err := disableHardwareOffloads("eth1"); err != nil {
			t.Fatalf("disableHardwareOffloads returned unexpected error: %v", err)
		}
		if !fe.closed {
			t.Errorf("expected ethtool handle to be closed")
		}
		if val, ok := fe.changed["tx-checksum-ip-generic"]; len(fe.changed) != 1 || !ok || val {
			t.Errorf("unexpected changed map: got %v, want map[tx-checksum-ip-generic:false]", fe.changed)
		}
	})

	t.Run("no-op when no target features are active", func(t *testing.T) {
		fe := &fakeEthtool{
			states: map[string]ethtool.FeatureState{
				"tx-checksum-ip-generic": {Available: true, Active: false, NeverChanged: false},
			},
		}
		newEthtool = func() (ethtoolClient, error) { return fe, nil }

		if err := disableHardwareOffloads("eth1"); err != nil {
			t.Fatalf("disableHardwareOffloads returned unexpected error: %v", err)
		}
		if fe.changed != nil {
			t.Errorf("expected Change not to be called, got %v", fe.changed)
		}
	})

	t.Run("propagates errors", func(t *testing.T) {
		newEthtool = func() (ethtoolClient, error) {
			return nil, fmt.Errorf("ethtool open failed")
		}
		if err := disableHardwareOffloads("eth1"); err == nil {
			t.Errorf("expected error when newEthtool fails, got nil")
		}

		fe := &fakeEthtool{statesErr: fmt.Errorf("ioctl failed")}
		newEthtool = func() (ethtoolClient, error) { return fe, nil }
		if err := disableHardwareOffloads("eth1"); err == nil {
			t.Errorf("expected error when FeaturesWithState fails, got nil")
		}
	})
}

func buildAuxDataOOB(status uint32) []byte {
	var data [20]byte
	binary.NativeEndian.PutUint32(data[:4], status)
	cmsgLen := unix.CmsgLen(len(data))
	buf := make([]byte, unix.CmsgSpace(len(data)))
	var hdr unix.Cmsghdr
	hdr.SetLen(cmsgLen)
	hdr.Level = unix.SOL_PACKET
	hdr.Type = unix.PACKET_AUXDATA
	binary.NativeEndian.PutUint64(buf[0:8], uint64(hdr.Len))
	binary.NativeEndian.PutUint32(buf[8:12], uint32(hdr.Level))
	binary.NativeEndian.PutUint32(buf[12:16], uint32(hdr.Type))
	copy(buf[unix.CmsgLen(0):], data[:])
	return buf
}

func TestPacketNeedsChecksum(t *testing.T) {
	if packetNeedsChecksum(nil) {
		t.Errorf("packetNeedsChecksum(nil) = true, want false")
	}
	if packetNeedsChecksum([]byte{0xff, 0x00, 0x01}) {
		t.Errorf("packetNeedsChecksum(malformed) = true, want false")
	}
	if packetNeedsChecksum(buildAuxDataOOB(unix.TP_STATUS_USER)) {
		t.Errorf("packetNeedsChecksum(TP_STATUS_USER) = true, want false")
	}
	if !packetNeedsChecksum(buildAuxDataOOB(unix.TP_STATUS_USER | unix.TP_STATUS_CSUMNOTREADY)) {
		t.Errorf("packetNeedsChecksum(TP_STATUS_CSUMNOTREADY) = false, want true")
	}
}

func TestFinalizePartialChecksumIPv4TCPAndUDP(t *testing.T) {
	// Construct an Ethernet + IPv4 + TCP frame with a bogus partial checksum (0x1234)
	payload := []byte("hello kne packet bridge")
	tcpLen := 20 + len(payload)
	ipTotalLen := 20 + tcpLen
	pkt := make([]byte, 14+ipTotalLen)

	// Ethernet header (EtherType IPv4 0x0800)
	binary.BigEndian.PutUint16(pkt[12:14], unix.ETH_P_IP)

	// IPv4 header
	ip := pkt[14 : 14+20]
	ip[0] = 0x45 // Version 4, IHL 5
	binary.BigEndian.PutUint16(ip[2:4], uint16(ipTotalLen))
	ip[8] = 64 // TTL
	ip[9] = unix.IPPROTO_TCP
	copy(ip[12:16], []byte{192, 0, 2, 1})
	copy(ip[16:20], []byte{192, 0, 2, 2})

	// TCP header + payload
	tcp := pkt[34:]
	binary.BigEndian.PutUint16(tcp[0:2], 12345)
	binary.BigEndian.PutUint16(tcp[2:4], 80)
	tcp[12] = 5 << 4              // Data offset 5
	tcp[16], tcp[17] = 0x12, 0x34 // Partial checksum placeholder
	copy(tcp[20:], payload)

	finalizePartialChecksum(pkt)

	if tcp[16] == 0x12 && tcp[17] == 0x34 {
		t.Fatalf("finalizePartialChecksum did not update TCP checksum")
	}

	// Verify RFC 1071 ones'-complement sum over pseudo-header + TCP segment is 0
	var sum uint32
	sum += uint32(binary.BigEndian.Uint16(ip[12:14])) + uint32(binary.BigEndian.Uint16(ip[14:16]))
	sum += uint32(binary.BigEndian.Uint16(ip[16:18])) + uint32(binary.BigEndian.Uint16(ip[18:20]))
	sum += uint32(unix.IPPROTO_TCP) + uint32(len(tcp))
	if rem := checksumData(sum, tcp); rem != 0 {
		t.Errorf("IPv4 TCP checksum verification failed: got remainder 0x%04x, want 0", rem)
	}

	// Also test IPv4 UDP with Ethernet trailer padding (60-byte minimum frame)
	udpPayload := []byte("hi")
	udpLen := 8 + len(udpPayload)
	ipTotalLenUDP := 20 + udpLen
	udpPkt := make([]byte, 60)
	binary.BigEndian.PutUint16(udpPkt[12:14], unix.ETH_P_IP)
	uip := udpPkt[14 : 14+20]
	uip[0] = 0x45
	binary.BigEndian.PutUint16(uip[2:4], uint16(ipTotalLenUDP))
	uip[8] = 64
	uip[9] = unix.IPPROTO_UDP
	copy(uip[12:16], []byte{192, 0, 2, 1})
	copy(uip[16:20], []byte{192, 0, 2, 2})

	udp := udpPkt[34 : 34+udpLen]
	binary.BigEndian.PutUint16(udp[0:2], 5000)
	binary.BigEndian.PutUint16(udp[2:4], 5001)
	binary.BigEndian.PutUint16(udp[4:6], uint16(udpLen))
	udp[6], udp[7] = 0xab, 0xcd
	copy(udp[8:], udpPayload)
	// Fill Ethernet trailer padding with non-zero bytes to ensure padding is excluded
	for i := 14 + ipTotalLenUDP; i < len(udpPkt); i++ {
		udpPkt[i] = 0xff
	}

	finalizePartialChecksum(udpPkt)

	var usum uint32
	usum += uint32(binary.BigEndian.Uint16(uip[12:14])) + uint32(binary.BigEndian.Uint16(uip[14:16]))
	usum += uint32(binary.BigEndian.Uint16(uip[16:18])) + uint32(binary.BigEndian.Uint16(uip[18:20]))
	usum += uint32(unix.IPPROTO_UDP) + uint32(len(udp))
	if rem := checksumData(usum, udp); rem != 0 {
		t.Errorf("IPv4 UDP checksum verification failed: got remainder 0x%04x, want 0", rem)
	}
}

func TestFinalizePartialChecksumIPv6AndVLAN(t *testing.T) {
	payload := []byte("ipv6 payload")

	t.Run("QinQ VLAN tagged IPv6 TCP with Hop-by-Hop extension header", func(t *testing.T) {
		extHdrLen := 8
		tcpLen := 20 + len(payload)
		ipv6PayloadLen := extHdrLen + tcpLen
		// 14 (Eth) + 8 (802.1ad + 802.1Q) + 40 (IPv6) + 8 (Hop-by-Hop) + tcpLen
		pkt := make([]byte, 14+8+40+ipv6PayloadLen)

		// Outer VLAN 0x88a8, Inner VLAN 0x8100, EtherType IPv6 0x86dd
		binary.BigEndian.PutUint16(pkt[12:14], unix.ETH_P_8021AD)
		binary.BigEndian.PutUint16(pkt[16:18], unix.ETH_P_8021Q)
		binary.BigEndian.PutUint16(pkt[20:22], unix.ETH_P_IPV6)

		ip6 := pkt[22 : 22+40]
		ip6[0] = 0x60
		binary.BigEndian.PutUint16(ip6[4:6], uint16(ipv6PayloadLen))
		ip6[6] = unix.IPPROTO_HOPOPTS
		ip6[7] = 64
		copy(ip6[8:24], net.ParseIP("2001:db8::1").To16())
		copy(ip6[24:40], net.ParseIP("2001:db8::2").To16())

		// Hop-by-Hop extension header (NextHdr = TCP, HdrExtLen = 0 -> 8 bytes)
		ext := pkt[62 : 62+8]
		ext[0] = unix.IPPROTO_TCP
		ext[1] = 0

		tcp := pkt[70:]
		binary.BigEndian.PutUint16(tcp[0:2], 443)
		binary.BigEndian.PutUint16(tcp[2:4], 54321)
		tcp[12] = 5 << 4
		tcp[16], tcp[17] = 0xde, 0xad
		copy(tcp[20:], payload)

		finalizePartialChecksum(pkt)

		var sum uint32
		for i := 0; i < 16; i += 2 {
			sum += uint32(binary.BigEndian.Uint16(ip6[8+i : 10+i]))
			sum += uint32(binary.BigEndian.Uint16(ip6[24+i : 26+i]))
		}
		sum += uint32(len(tcp)) + uint32(unix.IPPROTO_TCP)
		if rem := checksumData(sum, tcp); rem != 0 {
			t.Errorf("IPv6 TCP checksum verification failed: got remainder 0x%04x, want 0", rem)
		}
	})

	t.Run("IPv6 ICMPv6 with GSO zero payload length", func(t *testing.T) {
		icmpLen := 8 + len(payload)
		pkt := make([]byte, 14+40+icmpLen)
		binary.BigEndian.PutUint16(pkt[12:14], unix.ETH_P_IPV6)

		ip6 := pkt[14 : 14+40]
		ip6[0] = 0x60
		binary.BigEndian.PutUint16(ip6[4:6], 0) // GSO zero payload length
		ip6[6] = unix.IPPROTO_ICMPV6
		ip6[7] = 255
		copy(ip6[8:24], net.ParseIP("fe80::1").To16())
		copy(ip6[24:40], net.ParseIP("fe80::2").To16())

		icmp6 := pkt[54:]
		icmp6[0] = 128 // Echo Request
		icmp6[1] = 0
		icmp6[2], icmp6[3] = 0xbe, 0xef
		copy(icmp6[8:], payload)

		finalizePartialChecksum(pkt)

		var sum uint32
		for i := 0; i < 16; i += 2 {
			sum += uint32(binary.BigEndian.Uint16(ip6[8+i : 10+i]))
			sum += uint32(binary.BigEndian.Uint16(ip6[24+i : 26+i]))
		}
		sum += uint32(len(icmp6)) + uint32(unix.IPPROTO_ICMPV6)
		if rem := checksumData(sum, icmp6); rem != 0 {
			t.Errorf("IPv6 ICMPv6 checksum verification failed: got remainder 0x%04x, want 0", rem)
		}
	})
}

func TestFinalizePartialChecksumSkipsFragmentsAndMalformedFrames(t *testing.T) {
	// Fragmented IPv4 UDP packet should not have its checksum modified.
	pkt := make([]byte, 14+20+16)
	binary.BigEndian.PutUint16(pkt[12:14], unix.ETH_P_IP)
	ip := pkt[14:34]
	ip[0] = 0x45
	binary.BigEndian.PutUint16(ip[2:4], 36)
	binary.BigEndian.PutUint16(ip[6:8], 0x2000) // More Fragments (MF) set
	ip[9] = unix.IPPROTO_UDP
	udp := pkt[34:]
	udp[6], udp[7] = 0x11, 0x22

	finalizePartialChecksum(pkt)
	if udp[6] != 0x11 || udp[7] != 0x22 {
		t.Errorf("expected fragmented IPv4 packet checksum to remain untouched, got %02x%02x", udp[6], udp[7])
	}

	// Short / truncated frames must not panic.
	finalizePartialChecksum(nil)
	finalizePartialChecksum(make([]byte, 10))
	vlanTrunc := make([]byte, 16)
	binary.BigEndian.PutUint16(vlanTrunc[12:14], unix.ETH_P_8021Q)
	finalizePartialChecksum(vlanTrunc)
}
