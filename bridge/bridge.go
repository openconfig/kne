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

// Package bridge implements the KNE packet bridge Wire service daemon.
package bridge

import (
	"context"
	"encoding/binary"
	"fmt"
	"io"
	"net"
	"sync"
	"sync/atomic"

	"golang.org/x/sys/unix"
	"google.golang.org/grpc/metadata"
	"k8s.io/klog/v2"

	wpb "github.com/openconfig/kne/proto/wire"
)

const (
	maxFrameSize          = 65535
	channelBufferCap      = 1000
	socketBufferSizeBytes = 4 * 1024 * 1024 // 4 MB
)

// htons converts host byte order to network byte order in an endian-safe manner.
func htons(v uint16) int {
	var b [2]byte
	binary.BigEndian.PutUint16(b[:], v)
	return int(binary.NativeEndian.Uint16(b[:]))
}

// ReadWriter abstracts the physical or simulated raw packet I/O for an interface.
type ReadWriter interface {
	ReadPacket() ([]byte, error)
	WritePacket(pkt []byte) error
	Close() error
}

// SocketHandler manages a raw AF_PACKET socket bound to a specific Linux network interface.
type SocketHandler struct {
	ifaceName string
	fd        int
	closeOnce sync.Once
}

// NewSocketHandler creates and configures a raw AF_PACKET socket in promiscuous mode for the given interface.
func NewSocketHandler(ifaceName string) (*SocketHandler, error) {
	iface, err := net.InterfaceByName(ifaceName)
	if err != nil {
		return nil, fmt.Errorf("interface %s not found: %w", ifaceName, err)
	}

	proto := htons(unix.ETH_P_ALL)
	fd, err := unix.Socket(unix.AF_PACKET, unix.SOCK_RAW, proto)
	if err != nil {
		return nil, fmt.Errorf("failed to open raw socket for %s: %w", ifaceName, err)
	}

	if err := unix.SetsockoptInt(fd, unix.SOL_SOCKET, unix.SO_RCVBUF, socketBufferSizeBytes); err != nil {
		klog.Warningf("Failed to set SO_RCVBUF on %s: %v", ifaceName, err)
	}
	if err := unix.SetsockoptInt(fd, unix.SOL_SOCKET, unix.SO_SNDBUF, socketBufferSizeBytes); err != nil {
		klog.Warningf("Failed to set SO_SNDBUF on %s: %v", ifaceName, err)
	}

	sll := unix.SockaddrLinklayer{
		Protocol: uint16(proto),
		Ifindex:  iface.Index,
	}
	if err := unix.Bind(fd, &sll); err != nil {
		unix.Close(fd)
		return nil, fmt.Errorf("failed to bind raw socket to %s (index %d): %w", ifaceName, iface.Index, err)
	}

	mreq := unix.PacketMreq{
		Ifindex: int32(iface.Index),
		Type:    unix.PACKET_MR_PROMISC,
	}
	if err := unix.SetsockoptPacketMreq(fd, unix.SOL_PACKET, unix.PACKET_ADD_MEMBERSHIP, &mreq); err != nil {
		klog.Warningf("Failed to enable promiscuous mode on %s: %v", ifaceName, err)
	}

	return &SocketHandler{
		ifaceName: ifaceName,
		fd:        fd,
	}, nil
}

// ReadPacket reads a single raw Ethernet frame from the socket, ignoring outgoing echo frames.
func (s *SocketHandler) ReadPacket() ([]byte, error) {
	buf := make([]byte, maxFrameSize)
	for {
		n, from, err := unix.Recvfrom(s.fd, buf, 0)
		if err != nil {
			return nil, err
		}
		// Filter out locally transmitted echo frames (PACKET_OUTGOING) to prevent infinite loops.
		if sll, ok := from.(*unix.SockaddrLinklayer); ok {
			if sll.Pkttype == unix.PACKET_OUTGOING {
				continue
			}
		}
		pkt := make([]byte, n)
		copy(pkt, buf[:n])
		return pkt, nil
	}
}

// WritePacket writes a raw Ethernet frame directly to the network interface.
func (s *SocketHandler) WritePacket(pkt []byte) error {
	_, err := unix.Write(s.fd, pkt)
	return err
}

// Close closes the underlying raw socket file descriptor once.
func (s *SocketHandler) Close() error {
	var err error
	s.closeOnce.Do(func() {
		err = unix.Close(s.fd)
	})
	return err
}

// InterfaceDemux coordinates a single raw socket reader per interface with multiple gRPC clients.
// This design prevents socket buffer race conditions and distributes captured frames to all active subscribers.
type InterfaceDemux struct {
	ifaceName     string
	handler       ReadWriter
	onClose       func(ifaceName string)
	mu            sync.RWMutex
	listeners     map[chan []byte]struct{}
	droppedFrames atomic.Uint64
	ctx           context.Context
	cancel        context.CancelFunc
	closeOnce     sync.Once
}

// newInterfaceDemux constructs and starts a new InterfaceDemux for the specified interface.
func newInterfaceDemux(parentCtx context.Context, ifaceName string, handler ReadWriter, onClose func(ifaceName string)) *InterfaceDemux {
	ctx, cancel := context.WithCancel(parentCtx)
	d := &InterfaceDemux{
		ifaceName: ifaceName,
		handler:   handler,
		onClose:   onClose,
		listeners: make(map[chan []byte]struct{}),
		ctx:       ctx,
		cancel:    cancel,
	}
	// Unblock unix.Recvfrom immediately when context is cancelled.
	go func() {
		<-d.ctx.Done()
		d.closeHandler()
	}()
	go d.readLoop()
	return d
}

func (d *InterfaceDemux) closeHandler() {
	d.closeOnce.Do(func() {
		_ = d.handler.Close()
	})
}

// readLoop continuously reads frames from the raw socket and broadcasts them to all active subscribers.
// Senders use a non-blocking fan-out with telemetry to prevent a slow or disconnected gRPC subscriber from
// holding the RLock and deadlocking unsubscriptions or starving other subscribers of raw socket frames.
func (d *InterfaceDemux) readLoop() {
	defer func() {
		d.closeHandler()
		d.mu.Lock()
		for ch := range d.listeners {
			close(ch)
		}
		d.listeners = make(map[chan []byte]struct{})
		d.mu.Unlock()
		if d.onClose != nil {
			d.onClose(d.ifaceName)
		}
	}()

	for {
		select {
		case <-d.ctx.Done():
			return
		default:
			pkt, err := d.handler.ReadPacket()
			if err != nil {
				if d.ctx.Err() != nil {
					return
				}
				klog.Errorf("Error reading packet from %s: %v", d.ifaceName, err)
				return
			}
			d.mu.RLock()
			for ch := range d.listeners {
				select {
				case ch <- pkt:
				case <-d.ctx.Done():
					d.mu.RUnlock()
					return
				default:
					total := d.droppedFrames.Add(1)
					if total%1000 == 1 {
						klog.Warningf("[%s] Demux subscriber queue full (buffer %d)! Dropping egress frame (total dropped: %d)",
							d.ifaceName, channelBufferCap, total)
					}
				}
			}
			d.mu.RUnlock()
		}
	}
}

// subscribe registers a new channel to receive captured frames from this interface.
func (d *InterfaceDemux) subscribe() chan []byte {
	ch := make(chan []byte, channelBufferCap)
	d.mu.Lock()
	d.listeners[ch] = struct{}{}
	d.mu.Unlock()
	return ch
}

// unsubscribe removes a previously registered subscriber channel and closes it.
func (d *InterfaceDemux) unsubscribe(ch chan []byte) {
	d.mu.Lock()
	if _, ok := d.listeners[ch]; ok {
		delete(d.listeners, ch)
		close(ch)
	}
	d.mu.Unlock()
}

// close cancels the demux context and terminates the read loop.
func (d *InterfaceDemux) close() {
	d.cancel()
}

// Server implements the wpb.WireServer gRPC service.
type Server struct {
	wpb.UnimplementedWireServer

	ctx          context.Context
	cancel       context.CancelFunc
	mu           sync.Mutex
	demuxers     map[string]*InterfaceDemux
	socketOpener func(ifaceName string) (ReadWriter, error)
}

// NewServer creates a new Wire server instance.
func NewServer(ctx context.Context) *Server {
	srvCtx, cancel := context.WithCancel(ctx)
	return &Server{
		ctx:      srvCtx,
		cancel:   cancel,
		demuxers: make(map[string]*InterfaceDemux),
		socketOpener: func(ifaceName string) (ReadWriter, error) {
			return NewSocketHandler(ifaceName)
		},
	}
}

// SetSocketOpener overrides the default socket factory for hermetic testing.
func (s *Server) SetSocketOpener(opener func(ifaceName string) (ReadWriter, error)) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.socketOpener = opener
}

// getOrCreateDemux retrieves an existing demuxer for the interface or opens a new one.
// It attaches a teardown callback so dead demuxers are automatically purged from s.demuxers.
func (s *Server) getOrCreateDemux(ifaceName string) (*InterfaceDemux, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if d, exists := s.demuxers[ifaceName]; exists {
		return d, nil
	}

	handler, err := s.socketOpener(ifaceName)
	if err != nil {
		return nil, fmt.Errorf("failed to open socket for interface %s: %w", ifaceName, err)
	}

	d := newInterfaceDemux(s.ctx, ifaceName, handler, func(name string) {
		s.mu.Lock()
		delete(s.demuxers, name)
		s.mu.Unlock()
		klog.Infof("InterfaceDemux for %s cleaned up and removed from server cache", name)
	})
	s.demuxers[ifaceName] = d
	return d, nil
}

// Transmit handles bidirectional packet streaming over the Wire service.
// It extracts the target interface from gRPC incoming metadata ("interface"),
// streams egress packets from the interface to gRPC, and writes ingress gRPC packets to the interface.
func (s *Server) Transmit(stream wpb.Wire_TransmitServer) error {
	md, ok := metadata.FromIncomingContext(stream.Context())
	if !ok {
		return fmt.Errorf("no metadata found on incoming stream")
	}

	var ifaceName string
	if vals := md.Get("interface"); len(vals) > 0 {
		ifaceName = vals[0]
	} else if vals := md.Get("x-kne-interface"); len(vals) > 0 {
		ifaceName = vals[0]
	} else {
		return fmt.Errorf("missing 'interface' header in gRPC stream metadata")
	}

	demux, err := s.getOrCreateDemux(ifaceName)
	if err != nil {
		return fmt.Errorf("failed to get interface handler for %s: %w", ifaceName, err)
	}

	pktChan := demux.subscribe()
	defer demux.unsubscribe(pktChan)

	errChan := make(chan error, 2)

	// Egress loop: read captured packets from InterfaceDemux and send to gRPC client.
	go func() {
		for {
			select {
			case <-stream.Context().Done():
				errChan <- stream.Context().Err()
				return
			case pkt, ok := <-pktChan:
				if !ok {
					errChan <- io.EOF
					return
				}
				if err := stream.Send(&wpb.Packet{Data: pkt}); err != nil {
					errChan <- err
					return
				}
			}
		}
	}()

	// Ingress loop: receive packets from gRPC client and inject directly into raw network interface.
	go func() {
		for {
			req, err := stream.Recv()
			if err != nil {
				errChan <- err
				return
			}
			if pktData := req.GetData(); len(pktData) > 0 {
				if err := demux.handler.WritePacket(pktData); err != nil {
					errChan <- fmt.Errorf("failed to write packet to %s: %w", ifaceName, err)
					return
				}
			}
		}
	}()

	// Wait for stream termination or error.
	err = <-errChan
	if err == io.EOF || stream.Context().Err() != nil {
		return nil
	}
	return err
}

// Close closes all underlying raw sockets and demuxers.
func (s *Server) Close() error {
	s.cancel()
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, d := range s.demuxers {
		d.close()
	}
	return nil
}
