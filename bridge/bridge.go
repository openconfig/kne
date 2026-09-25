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

	"github.com/safchain/ethtool"
	"golang.org/x/sys/unix"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
	"k8s.io/klog/v2"

	wpb "github.com/openconfig/kne/proto/wire"
)

const (
	maxFrameSize           = 65535
	oobBufferSize          = 128
	channelBufferCap       = 1000
	maxSubscribersPerDemux = 32
	socketBufferSizeBytes  = 4 * 1024 * 1024 // 4 MB
)

var offloadDisabledMap = map[string]bool{
	"tx-checksum-ipv4":             false,
	"tx-checksum-ipv6":             false,
	"tx-checksum-ip-generic":       false,
	"tx-tcp-segmentation":          false,
	"tx-tcp6-segmentation":         false,
	"tx-checksum-fcoe-crc":         false,
	"tx-checksum-sctp":             false,
	"tx-tcp-ecn-segmentation":      false,
	"tx-tcp-mangleid-segmentation": false,
	"tx-generic-segmentation":      false,
	"tx-udp-segmentation":          false,
	"rx-gro":                       false,
	"rx-lro":                       false,
	"rx-checksum":                  false,
}

const (
	ethP8021QQinQ = 0x9100
	ipv4FragMask  = 0x3fff // More Fragments (0x2000) | Fragment Offset (0x1fff)
)

type ethtoolClient interface {
	FeaturesWithState(intf string) (map[string]ethtool.FeatureState, error)
	Change(intf string, config map[string]bool) error
	Close()
}

var newEthtool = func() (ethtoolClient, error) {
	return ethtool.NewEthtool()
}

// disableHardwareOffloads disables TX/RX checksum, segmentation (TSO/GSO/USO), and receive
// coalescing (GRO/LRO) offloads on the specified interface using ethtool so captured frames
// are not coalesced or left with partial checksums.
func disableHardwareOffloads(ifaceName string) error {
	etlHndl, err := newEthtool()
	if err != nil {
		return fmt.Errorf("could not open ethtool handle: %w", err)
	}
	defer etlHndl.Close()

	states, err := etlHndl.FeaturesWithState(ifaceName)
	if err != nil {
		return fmt.Errorf("could not query ethtool features for %s: %w", ifaceName, err)
	}

	cfg := make(map[string]bool, len(offloadDisabledMap))
	for k, v := range offloadDisabledMap {
		if st, ok := states[k]; ok && st.Available && !st.NeverChanged && st.Active {
			cfg[k] = v
		}
	}
	if len(cfg) == 0 {
		return nil
	}
	return etlHndl.Change(ifaceName, cfg)
}

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
	oobBuf    []byte
	closeOnce sync.Once
}

// NewSocketHandler creates and configures a raw AF_PACKET socket in promiscuous mode for the given interface.
func NewSocketHandler(ifaceName string) (*SocketHandler, error) {
	iface, err := net.InterfaceByName(ifaceName)
	if err != nil {
		return nil, fmt.Errorf("interface %s not found: %w", ifaceName, err)
	}

	if err := disableHardwareOffloads(ifaceName); err != nil {
		klog.Warningf("Failed to disable hardware offloads on %s: %v", ifaceName, err)
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
	if err := unix.SetsockoptInt(fd, unix.SOL_PACKET, unix.PACKET_AUXDATA, 1); err != nil {
		klog.Warningf("Failed to enable PACKET_AUXDATA on %s: %v", ifaceName, err)
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
		oobBuf:    make([]byte, oobBufferSize),
	}, nil
}

// ReadPacket reads a single raw Ethernet frame from the socket, ignoring outgoing echo frames.
func (s *SocketHandler) ReadPacket() ([]byte, error) {
	for {
		var n, oobn int
		var from unix.Sockaddr
		var serr error
		if rerr := s.rc.Read(func(fd uintptr) bool {
			for {
				n, oobn, _, from, serr = unix.Recvmsg(int(fd), s.rbuf, s.oobBuf, 0)
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
		if packetNeedsChecksum(s.oobBuf[:oobn]) {
			finalizePartialChecksum(pkt)
		}
		return pkt, nil
	}
}

// packetNeedsChecksum parses AF_PACKET PACKET_AUXDATA control messages and returns true
// if TP_STATUS_CSUMNOTREADY (skb->ip_summed == CHECKSUM_PARTIAL) is set.
func packetNeedsChecksum(oob []byte) bool {
	if len(oob) == 0 {
		return false
	}
	cmsgs, err := unix.ParseSocketControlMessage(oob)
	if err != nil {
		return false
	}
	for _, cmsg := range cmsgs {
		if cmsg.Header.Level == unix.SOL_PACKET && cmsg.Header.Type == unix.PACKET_AUXDATA && len(cmsg.Data) >= 4 {
			status := binary.NativeEndian.Uint32(cmsg.Data[:4])
			if status&unix.TP_STATUS_CSUMNOTREADY != 0 {
				return true
			}
		}
	}
	return false
}

// finalizePartialChecksum computes and writes the L4 (TCP/UDP/ICMPv6) checksum in software
// for an Ethernet frame captured with CHECKSUM_PARTIAL (TP_STATUS_CSUMNOTREADY).
func finalizePartialChecksum(pkt []byte) {
	if len(pkt) < 14 {
		return
	}
	etherType := binary.BigEndian.Uint16(pkt[12:14])
	l3Offset := 14
	for i := 0; i < 2 && (etherType == unix.ETH_P_8021Q || etherType == unix.ETH_P_8021AD || etherType == ethP8021QQinQ); i++ {
		if len(pkt) < l3Offset+4 {
			return
		}
		etherType = binary.BigEndian.Uint16(pkt[l3Offset+2 : l3Offset+4])
		l3Offset += 4
	}

	switch etherType {
	case unix.ETH_P_IP:
		if len(pkt) < l3Offset+20 || pkt[l3Offset]>>4 != 4 {
			return
		}
		ihl := int(pkt[l3Offset]&0x0f) * 4
		if ihl < 20 || len(pkt) < l3Offset+ihl {
			return
		}
		if binary.BigEndian.Uint16(pkt[l3Offset+6:l3Offset+8])&ipv4FragMask != 0 {
			return
		}
		totalLen := int(binary.BigEndian.Uint16(pkt[l3Offset+2 : l3Offset+4]))
		l3End := l3Offset + totalLen
		if totalLen < ihl || l3End > len(pkt) {
			l3End = len(pkt)
		}
		proto := pkt[l3Offset+9]
		srcIP := pkt[l3Offset+12 : l3Offset+16]
		dstIP := pkt[l3Offset+16 : l3Offset+20]
		l4 := pkt[l3Offset+ihl : l3End]

		var sum uint32
		sum += uint32(binary.BigEndian.Uint16(srcIP[0:2])) + uint32(binary.BigEndian.Uint16(srcIP[2:4]))
		sum += uint32(binary.BigEndian.Uint16(dstIP[0:2])) + uint32(binary.BigEndian.Uint16(dstIP[2:4]))
		sum += uint32(proto) + uint32(len(l4))

		writeL4Checksum(proto, sum, l4, false)

	case unix.ETH_P_IPV6:
		if len(pkt) < l3Offset+40 || pkt[l3Offset]>>4 != 6 {
			return
		}
		payloadLen := int(binary.BigEndian.Uint16(pkt[l3Offset+4 : l3Offset+6]))
		l3End := l3Offset + 40 + payloadLen
		if payloadLen == 0 || l3End > len(pkt) {
			l3End = len(pkt)
		}
		nextHdr := pkt[l3Offset+6]
		srcIP := pkt[l3Offset+8 : l3Offset+24]
		dstIP := pkt[l3Offset+24 : l3Offset+40]
		l4Offset := l3Offset + 40

		for nextHdr == unix.IPPROTO_HOPOPTS || nextHdr == unix.IPPROTO_ROUTING || nextHdr == unix.IPPROTO_DSTOPTS {
			if l4Offset+2 > l3End {
				return
			}
			extLen := (int(pkt[l4Offset+1]) + 1) * 8
			nextHdr = pkt[l4Offset]
			l4Offset += extLen
		}
		if l4Offset > l3End {
			return
		}
		l4 := pkt[l4Offset:l3End]

		var sum uint32
		for i := 0; i < 16; i += 2 {
			sum += uint32(binary.BigEndian.Uint16(srcIP[i : i+2]))
			sum += uint32(binary.BigEndian.Uint16(dstIP[i : i+2]))
		}
		l4Len := uint32(len(l4))
		sum += (l4Len >> 16) + (l4Len & 0xffff) + uint32(nextHdr)

		writeL4Checksum(nextHdr, sum, l4, true)
	}
}

func writeL4Checksum(proto uint8, pseudoSum uint32, l4 []byte, allowICMPv6 bool) {
	switch proto {
	case unix.IPPROTO_TCP:
		if len(l4) < 20 {
			return
		}
		l4[16], l4[17] = 0, 0
		binary.BigEndian.PutUint16(l4[16:18], checksumData(pseudoSum, l4))
	case unix.IPPROTO_UDP:
		if len(l4) < 8 {
			return
		}
		l4[6], l4[7] = 0, 0
		csum := checksumData(pseudoSum, l4)
		if csum == 0 {
			csum = 0xffff
		}
		binary.BigEndian.PutUint16(l4[6:8], csum)
	case unix.IPPROTO_ICMPV6:
		if !allowICMPv6 || len(l4) < 4 {
			return
		}
		l4[2], l4[3] = 0, 0
		binary.BigEndian.PutUint16(l4[2:4], checksumData(pseudoSum, l4))
	}
}

func checksumData(sum uint32, data []byte) uint16 {
	i := 0
	for ; i+1 < len(data); i += 2 {
		sum += uint32(binary.BigEndian.Uint16(data[i : i+2]))
	}
	if i < len(data) {
		sum += uint32(data[i]) << 8
	}
	for sum > 0xffff {
		sum = (sum >> 16) + (sum & 0xffff)
	}
	return ^uint16(sum)
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
	var egressWG sync.WaitGroup
	defer func() {
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

	// Egress loop: read captured packets from InterfaceDemux and send to gRPC client.
	egressWG.Add(1)
	go func() {
		defer egressWG.Done()
		for {
			select {
			case <-stream.Context().Done():
				select {
				case errChan <- stream.Context().Err():
				default:
				}
				return
			case pkt, ok := <-pktChan:
				if !ok {
					select {
					case errChan <- io.EOF:
					default:
					}
					return
				}
				if err := stream.Send(&wpb.Packet{Data: pkt}); err != nil {
					select {
					case errChan <- err:
					default:
					}
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
				select {
				case errChan <- err:
				default:
				}
				return
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
// as opposed to a transient or per-packet error (e.g. EMSGSIZE, ENOBUFS).
func isFatalWriteError(err error) bool {
	if err == nil {
		return false
	}
	if errors.Is(err, net.ErrClosed) || errors.Is(err, io.EOF) || errors.Is(err, os.ErrClosed) ||
		errors.Is(err, unix.EBADF) || errors.Is(err, unix.ENETDOWN) || errors.Is(err, unix.ENODEV) || errors.Is(err, unix.ESHUTDOWN) {
		return true
	}
	var errno unix.Errno
	if errors.As(err, &errno) {
		switch errno {
		case unix.EBADF, unix.ENETDOWN, unix.ENODEV, unix.ESHUTDOWN, unix.EINVAL:
			return true
		default:
			return false
		}
	}
	return false
}
