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
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"sync"
	"sync/atomic"
	"syscall"

	"golang.org/x/sys/unix"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
	"k8s.io/klog/v2"

	wpb "github.com/openconfig/kne/proto/wire"
)

const (
	maxFrameSize           = 65535
	channelBufferCap       = 1000
	maxSubscribersPerDemux = 32
	socketBufferSizeBytes  = 4 * 1024 * 1024 // 4 MB
)

// htons converts host byte order to network byte order in an endian-safe manner.
func htons(v uint16) int {
	var b [2]byte
	binary.BigEndian.PutUint16(b[:], v)
	return int(binary.NativeEndian.Uint16(b[:]))
}

// ReadWriter abstracts the physical or simulated raw packet I/O for an interface.
// Implementations MUST ensure that Close() interrupts any pending or concurrent ReadPacket() calls.
type ReadWriter interface {
	ReadPacket() ([]byte, error)
	WritePacket(pkt []byte) error
	Close() error
}

// SocketHandler manages a raw AF_PACKET socket bound to a specific Linux network interface.
// It integrates with the Go runtime netpoller so that ReadPacket blocks without spinning,
// and Close() immediately interrupts pending reads and prevents use-after-close errors.
type SocketHandler struct {
	ifaceName string
	f         *os.File
	rc        syscall.RawConn
	rbuf      []byte
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

	if err := unix.SetNonblock(fd, true); err != nil {
		_ = unix.Close(fd)
		return nil, fmt.Errorf("failed to set non-blocking on %s: %w", ifaceName, err)
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
		_ = unix.Close(fd)
		return nil, fmt.Errorf("failed to bind raw socket to %s (index %d): %w", ifaceName, iface.Index, err)
	}

	mreq := unix.PacketMreq{
		Ifindex: int32(iface.Index),
		Type:    unix.PACKET_MR_PROMISC,
	}
	if err := unix.SetsockoptPacketMreq(fd, unix.SOL_PACKET, unix.PACKET_ADD_MEMBERSHIP, &mreq); err != nil {
		klog.Warningf("Failed to enable promiscuous mode on %s: %v", ifaceName, err)
	}

	f := os.NewFile(uintptr(fd), "packet-"+ifaceName)
	rc, err := f.SyscallConn()
	if err != nil {
		_ = f.Close()
		return nil, fmt.Errorf("failed to get SyscallConn on %s: %w", ifaceName, err)
	}

	return &SocketHandler{
		ifaceName: ifaceName,
		f:         f,
		rc:        rc,
		rbuf:      make([]byte, maxFrameSize),
	}, nil
}

// ReadPacket reads a single raw Ethernet frame from the socket, ignoring outgoing echo frames.
func (s *SocketHandler) ReadPacket() ([]byte, error) {
	for {
		var n int
		var from unix.Sockaddr
		var serr error
		if rerr := s.rc.Read(func(fd uintptr) bool {
			for {
				n, from, serr = unix.Recvfrom(int(fd), s.rbuf, 0)
				if serr == unix.EINTR {
					continue
				}
				return serr != unix.EAGAIN
			}
		}); rerr != nil {
			return nil, rerr
		}
		if serr != nil {
			return nil, serr
		}
		// Filter out locally transmitted echo frames (PACKET_OUTGOING) to prevent infinite loops.
		if sll, ok := from.(*unix.SockaddrLinklayer); ok {
			if sll.Pkttype == unix.PACKET_OUTGOING {
				continue
			}
		}
		pkt := make([]byte, n)
		copy(pkt, s.rbuf[:n])
		return pkt, nil
	}
}

// WritePacket writes a raw Ethernet frame directly to the network interface.
func (s *SocketHandler) WritePacket(pkt []byte) error {
	if len(pkt) == 0 {
		return nil
	}
	if len(pkt) > maxFrameSize {
		return fmt.Errorf("packet size %d exceeds max frame size %d", len(pkt), maxFrameSize)
	}
	var serr error
	if werr := s.rc.Write(func(fd uintptr) bool {
		for {
			_, serr = unix.Write(int(fd), pkt)
			if serr == unix.EINTR {
				continue
			}
			return serr != unix.EAGAIN
		}
	}); werr != nil {
		return werr
	}
	return serr
}

// Close closes the underlying raw socket file descriptor once.
// Closing interrupts any blocked or future ReadPacket / WritePacket calls via the runtime netpoller.
func (s *SocketHandler) Close() error {
	var err error
	s.closeOnce.Do(func() {
		err = s.f.Close()
	})
	return err
}

// InterfaceDemux coordinates a single raw socket reader per interface with multiple gRPC clients.
// This design prevents socket buffer race conditions and distributes captured frames to all active subscribers.
// It also distributes the link state of the interface to all subscribers and applies link state received from
// any of them.
type InterfaceDemux struct {
	ifaceName          string
	handler            ReadWriter
	onClose            func(ifaceName string)
	mu                 sync.RWMutex
	listeners          map[chan []byte]struct{}
	closed             bool
	droppedFrames      atomic.Uint64
	droppedWriteFrames atomic.Uint64
	ctx                context.Context
	cancel             context.CancelFunc
	closeOnce          sync.Once
	closeErr           error
	wg                 sync.WaitGroup

	// linkMu guards the link-state fields below.
	linkMu        sync.Mutex
	link          Link // nil if link state is not available
	linkClosed    bool
	reporter      linkReporter
	linkListeners map[chan wpb.LinkState]struct{}
}

// newInterfaceDemux constructs and starts a new InterfaceDemux for the specified interface.
func newInterfaceDemux(parentCtx context.Context, ifaceName string, handler ReadWriter, onClose func(ifaceName string)) *InterfaceDemux {
	ctx, cancel := context.WithCancel(parentCtx)
	d := &InterfaceDemux{
		ifaceName:     ifaceName,
		handler:       handler,
		onClose:       onClose,
		listeners:     make(map[chan []byte]struct{}),
		linkListeners: make(map[chan wpb.LinkState]struct{}),
		ctx:           ctx,
		cancel:        cancel,
	}
	// Unblock pending ReadPacket calls when demux context is cancelled.
	go func() {
		<-d.ctx.Done()
		_ = d.closeHandler()
	}()
	d.wg.Add(1)
	go func() {
		defer d.wg.Done()
		d.readLoop()
	}()
	return d
}

func (d *InterfaceDemux) closeHandler() error {
	d.closeOnce.Do(func() {
		d.closeErr = d.handler.Close()
	})
	return d.closeErr
}

// write writes an ingress packet to the underlying interface handler after verifying that the demux is active.
func (d *InterfaceDemux) write(pkt []byte) error {
	if err := d.ctx.Err(); err != nil {
		return net.ErrClosed
	}
	if len(pkt) == 0 {
		return nil
	}
	if len(pkt) > maxFrameSize {
		d.droppedWriteFrames.Add(1)
		return fmt.Errorf("packet size %d exceeds max frame size %d", len(pkt), maxFrameSize)
	}
	return d.handler.WritePacket(pkt)
}

// readLoop continuously reads frames from the raw socket and broadcasts them to all active subscribers.
// Senders use a non-blocking fan-out with telemetry to prevent a slow or disconnected gRPC subscriber from
// holding the RLock and deadlocking unsubscriptions or starving other subscribers of raw socket frames.
func (d *InterfaceDemux) readLoop() {
	defer func() {
		d.cancel()
		_ = d.closeHandler()
		d.mu.Lock()
		d.closed = true
		for ch := range d.listeners {
			close(ch)
		}
		d.listeners = make(map[chan []byte]struct{})
		d.mu.Unlock()
		d.closeLink()
		if d.onClose != nil {
			d.onClose(d.ifaceName)
		}
	}()

	for {
		pkt, err := d.handler.ReadPacket()
		if err != nil {
			if d.ctx.Err() != nil {
				return
			}
			if errors.Is(err, unix.ENETDOWN) {
				// An AF_PACKET socket reports ENETDOWN once when its interface goes down, which
				// happens when link state DOWN is propagated, and resumes when it comes back up.
				klog.V(1).Infof("Interface %s went down; waiting for it to come back up", d.ifaceName)
				continue
			}
			klog.Errorf("Error reading packet from %s: %v", d.ifaceName, err)
			return
		}
		if d.ctx.Err() != nil {
			return
		}

		d.mu.RLock()
		if d.closed {
			d.mu.RUnlock()
			return
		}
		for ch := range d.listeners {
			select {
			case ch <- pkt:
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

// subscribe registers a new channel to receive captured frames from this interface.
// Note: The returned channel receives slices sharing the same underlying packet data array across all
// concurrent subscribers; callers must treat received []byte slices as immutable.
func (d *InterfaceDemux) subscribe() (chan []byte, error) {
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.closed || d.ctx.Err() != nil {
		return nil, fmt.Errorf("demux for %s is closed", d.ifaceName)
	}
	if len(d.listeners) >= maxSubscribersPerDemux {
		return nil, fmt.Errorf("demux for %s reached maximum subscribers limit (%d)", d.ifaceName, maxSubscribersPerDemux)
	}
	ch := make(chan []byte, channelBufferCap)
	d.listeners[ch] = struct{}{}
	return ch, nil
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
	_ = d.closeHandler()
}

// attachLink makes the demux distribute the link state of l to its subscribers and apply link state received
// from them to l. The demux closes l when it terminates.
func (d *InterfaceDemux) attachLink(l Link) {
	d.linkMu.Lock()
	if d.linkClosed {
		d.linkMu.Unlock()
		_ = l.Close()
		return
	}
	d.link = l
	if obs, err := l.Current(); err == nil {
		d.reporter.observe(obs)
	} else {
		klog.Warningf("Failed to read link state of %s: %v", d.ifaceName, err)
	}
	d.linkMu.Unlock()

	d.wg.Add(1)
	go func() {
		defer d.wg.Done()
		for obs := range l.Events() {
			d.observeLink(obs)
		}
	}()
}

// closeLink stops observing the link state of the interface.
func (d *InterfaceDemux) closeLink() {
	d.linkMu.Lock()
	l := d.link
	d.linkClosed = true
	d.linkMu.Unlock()
	if l != nil {
		_ = l.Close()
	}
}

// observeLink distributes the link state to report for obs to all link-state subscribers.
func (d *InterfaceDemux) observeLink(obs LinkObservation) {
	d.linkMu.Lock()
	defer d.linkMu.Unlock()
	s := d.reporter.observe(obs)
	if s == wpb.LinkState_LINK_STATE_UNKNOWN {
		return
	}
	klog.Infof("[%s] Link state %v (admin up=%t, carrier=%t)", d.ifaceName, s, obs.AdminUp, obs.Carrier)
	for ch := range d.linkListeners {
		publishLatest(ch, s)
	}
}

// subscribeLinkState registers a new channel to receive the link state to report to a peer. The channel holds
// only the latest state, and initially holds the current state if there is one to report.
func (d *InterfaceDemux) subscribeLinkState() chan wpb.LinkState {
	ch := make(chan wpb.LinkState, 1)
	d.linkMu.Lock()
	defer d.linkMu.Unlock()
	d.linkListeners[ch] = struct{}{}
	if s := d.reporter.current(); s != wpb.LinkState_LINK_STATE_UNKNOWN {
		ch <- s
	}
	return ch
}

// unsubscribeLinkState removes a channel registered by subscribeLinkState.
func (d *InterfaceDemux) unsubscribeLinkState(ch chan wpb.LinkState) {
	d.linkMu.Lock()
	defer d.linkMu.Unlock()
	delete(d.linkListeners, ch)
}

// setLinkState applies link state received from a peer by setting the interface administratively up or down.
func (d *InterfaceDemux) setLinkState(s wpb.LinkState) {
	d.linkMu.Lock()
	l := d.link
	d.linkMu.Unlock()
	if l == nil {
		klog.V(1).Infof("[%s] Ignoring link state %v: link state is not available", d.ifaceName, s)
		return
	}
	up := s == wpb.LinkState_LINK_STATE_UP
	klog.Infof("[%s] Setting admin up=%t for received link state %v", d.ifaceName, up, s)
	if err := l.SetAdminUp(up); err != nil {
		klog.Errorf("[%s] Failed to apply link state %v: %v", d.ifaceName, s, err)
	}
}

// wait blocks until the read loop has finished and returns the socket close error.
func (d *InterfaceDemux) wait() error {
	d.wg.Wait()
	return d.closeErr
}

// Server implements the wpb.WireServer gRPC service.
type Server struct {
	wpb.UnimplementedWireServer

	ctx          context.Context
	cancel       context.CancelFunc
	mu           sync.Mutex
	demuxers     map[string]*InterfaceDemux
	socketOpener func(ifaceName string) (ReadWriter, error)
	linkOpener   func(ifaceName string) (Link, error)
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
		linkOpener: NewNetlinkLink,
	}
}

// SetSocketOpener overrides the default socket factory for hermetic testing.
func (s *Server) SetSocketOpener(opener func(ifaceName string) (ReadWriter, error)) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.socketOpener = opener
}

// SetLinkOpener overrides the default link-state factory for hermetic testing. A nil opener disables link
// state.
func (s *Server) SetLinkOpener(opener func(ifaceName string) (Link, error)) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.linkOpener = opener
}

// getOrCreateDemux retrieves an existing demuxer for the interface or opens a new one.
// It attaches a teardown callback so dead demuxers are automatically purged from s.demuxers.
func (s *Server) getOrCreateDemux(ifaceName string) (*InterfaceDemux, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if err := s.ctx.Err(); err != nil {
		return nil, status.Errorf(codes.Unavailable, "bridge server is shutting down: %v", err)
	}

	if d, ok := s.demuxers[ifaceName]; ok {
		if d.ctx.Err() == nil {
			return d, nil
		}
		delete(s.demuxers, ifaceName)
	}

	handler, err := s.socketOpener(ifaceName)
	if err != nil {
		return nil, fmt.Errorf("failed to open socket for interface %s: %w", ifaceName, err)
	}

	var d *InterfaceDemux
	d = newInterfaceDemux(s.ctx, ifaceName, handler, func(name string) {
		s.mu.Lock()
		if cur, ok := s.demuxers[name]; ok && cur == d {
			delete(s.demuxers, name)
		}
		s.mu.Unlock()
		klog.Infof("InterfaceDemux for %s cleaned up and removed from server cache", name)
	})
	s.demuxers[ifaceName] = d
	if s.linkOpener != nil {
		if l, err := s.linkOpener(ifaceName); err != nil {
			klog.Warningf("Link state of %s will not be propagated: %v", ifaceName, err)
		} else {
			d.attachLink(l)
		}
	}
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
		return err
	}

	pktChan, err := demux.subscribe()
	if err != nil {
		return status.Errorf(codes.Unavailable, "failed to subscribe to interface %s: %v", ifaceName, err)
	}
	linkChan := demux.subscribeLinkState()
	var egressWG sync.WaitGroup
	defer func() {
		demux.unsubscribeLinkState(linkChan)
		demux.unsubscribe(pktChan)
		egressWG.Wait()
		klog.Infof("Wire.Transmit client disconnected from interface %q", ifaceName)
	}()

	// Send stream header immediately to flush HTTP/2 response headers so the client's stream.Header() unblocks.
	if err := stream.SendHeader(metadata.MD{}); err != nil {
		return fmt.Errorf("failed to send stream header for %s: %w", ifaceName, err)
	}
	klog.Infof("Wire.Transmit client connected for interface %q", ifaceName)

	errChan := make(chan error, 2)

	// Egress loop: read captured packets and link-state changes from InterfaceDemux and send them to the gRPC
	// client. This is the only goroutine that sends on the stream. The current link state, if any, is already
	// queued in linkChan, so it is sent right after the header.
	egressWG.Add(1)
	go func() {
		defer egressWG.Done()
		for {
			var msg *wpb.Packet
			select {
			case <-stream.Context().Done():
				select {
				case errChan <- stream.Context().Err():
				default:
				}
				return
			case ls := <-linkChan:
				msg = &wpb.Packet{LinkState: ls}
			case pkt, ok := <-pktChan:
				if !ok {
					select {
					case errChan <- io.EOF:
					default:
					}
					return
				}
				msg = &wpb.Packet{Data: pkt}
			}
			if err := stream.Send(msg); err != nil {
				select {
				case errChan <- err:
				default:
				}
				return
			}
		}
	}()

	// Ingress loop: receive packets from gRPC client and inject directly into raw network interface, and apply
	// received link state to the interface.
	go func() {
		for {
			req, err := stream.Recv()
			if err != nil {
				select {
				case errChan <- err:
				default:
				}
				return
			}
			if ls := req.GetLinkState(); ls != wpb.LinkState_LINK_STATE_UNKNOWN {
				demux.setLinkState(ls)
			}
			if pktData := req.GetData(); len(pktData) > 0 {
				if err := demux.write(pktData); err != nil {
					if isFatalWriteError(err) {
						select {
						case errChan <- fmt.Errorf("fatal error writing packet to %s: %w", ifaceName, err):
						default:
						}
						return
					}
					total := demux.droppedWriteFrames.Add(1)
					if total%1000 == 1 {
						klog.Warningf("[%s] Dropped ingress frame: %v (total dropped: %d)", ifaceName, err, total)
					}
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

// Close closes all underlying raw sockets and demuxers, waiting for read loops to finish.
func (s *Server) Close() error {
	s.cancel()
	s.mu.Lock()
	ds := make([]*InterfaceDemux, 0, len(s.demuxers))
	for _, d := range s.demuxers {
		ds = append(ds, d)
	}
	s.mu.Unlock()

	var errs []error
	for _, d := range ds {
		d.close()
		if err := d.wait(); err != nil {
			errs = append(errs, err)
		}
	}
	return errors.Join(errs...)
}

// isFatalWriteError determines if a write error is permanent and indicates socket/stream failure,
// as opposed to a transient or per-packet error (e.g. EMSGSIZE, ENOBUFS). ENETDOWN is not fatal:
// the interface is down, typically because link state DOWN was propagated, and frames written
// while it is down are dropped until it comes back up.
func isFatalWriteError(err error) bool {
	if err == nil {
		return false
	}
	if errors.Is(err, net.ErrClosed) || errors.Is(err, io.EOF) || errors.Is(err, os.ErrClosed) ||
		errors.Is(err, unix.EBADF) || errors.Is(err, unix.ENODEV) || errors.Is(err, unix.ESHUTDOWN) {
		return true
	}
	var errno unix.Errno
	if errors.As(err, &errno) {
		switch errno {
		case unix.EBADF, unix.ENODEV, unix.ESHUTDOWN, unix.EINVAL:
			return true
		default:
			return false
		}
	}
	return false
}
