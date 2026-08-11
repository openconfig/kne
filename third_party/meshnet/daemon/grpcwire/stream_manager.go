package grpcwire

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"

	mpb "github.com/openconfig/kne/third_party/meshnet/daemon/proto/meshnet/v1beta1"
	"github.com/openconfig/kne/third_party/meshnet/utils/wireutil"
)

type nodeStreamKey struct {
	topoNs string
	peerIP string
}

// NodeStream represents a single multiplexed gRPC packet stream shared by all TAP interfaces bound to a peer node within a topology.
type NodeStream struct {
	key      nodeStreamKey
	pktChan  chan *mpb.Packet
	stopChan chan struct{}
	refCount int
}

type nodeStreamManager struct {
	mu      sync.Mutex
	streams map[nodeStreamKey]*NodeStream
}

var streamMgr = &nodeStreamManager{
	streams: make(map[nodeStreamKey]*NodeStream),
}

// GetOrCreateStream returns (or creates) the multiplexed NodeStream for the given topology namespace and peer IP.
// Increments refCount.
func (m *nodeStreamManager) GetOrCreateStream(topoNs string, peerIP string) *NodeStream {
	if topoNs == "" {
		topoNs = "default"
	}

	m.mu.Lock()
	defer m.mu.Unlock()

	key := nodeStreamKey{
		topoNs: topoNs,
		peerIP: peerIP,
	}

	if st, ok := m.streams[key]; ok {
		st.refCount++
		return st
	}

	st := &NodeStream{
		key:      key,
		pktChan:  make(chan *mpb.Packet, 10000),
		stopChan: make(chan struct{}),
		refCount: 1,
	}
	m.streams[key] = st

	go st.run()
	return st
}

// ReleaseStream decrements refCount and stops the NodeStream if refCount reaches 0.
func (m *nodeStreamManager) ReleaseStream(topoNs string, peerIP string) {
	if topoNs == "" {
		topoNs = "default"
	}

	m.mu.Lock()
	defer m.mu.Unlock()

	key := nodeStreamKey{
		topoNs: topoNs,
		peerIP: peerIP,
	}

	st, ok := m.streams[key]
	if !ok {
		return
	}

	st.refCount--
	if st.refCount <= 0 {
		close(st.stopChan)
		delete(m.streams, key)
	}
}

// Send enqueues a packet payload to be transmitted over the multiplexed gRPC stream for the given topo and peer IP.
func (m *nodeStreamManager) Send(topoNs string, peerIP string, pkt *mpb.Packet) bool {
	if peerIP == "" || pkt == nil {
		return false
	}
	if topoNs == "" {
		topoNs = "default"
	}
	key := nodeStreamKey{
		topoNs: topoNs,
		peerIP: peerIP,
	}

	m.mu.Lock()
	st, ok := m.streams[key]
	if !ok {
		st = &NodeStream{
			key:      key,
			pktChan:  make(chan *mpb.Packet, 10000),
			stopChan: make(chan struct{}),
			refCount: 1,
		}
		m.streams[key] = st
		go st.run()
	}
	m.mu.Unlock()

	return st.Send(pkt)
}

// Send enqueues a packet payload to be transmitted over the multiplexed gRPC stream.
func (s *NodeStream) Send(pkt *mpb.Packet) bool {
	if pkt == nil {
		return false
	}
	frameCopy := make([]byte, len(pkt.Frame))
	copy(frameCopy, pkt.Frame)
	pktCopy := &mpb.Packet{
		RemotIntfId: pkt.RemotIntfId,
		Frame:       frameCopy,
	}
	select {
	case s.pktChan <- pktCopy:
		return true
	default:
		// Queue full; drop packet under extreme overload
		return false
	}
}

func (s *NodeStream) run() {
	url := strings.TrimSpace(fmt.Sprintf("%s:%d", s.key.peerIP, wireutil.GRPCDefaultPort))
	dialOpts := []grpc.DialOption{
		grpc.WithTransportCredentials(insecure.NewCredentials()),
		grpc.WithInitialWindowSize(4 * 1024 * 1024),      // 4MB stream window
		grpc.WithInitialConnWindowSize(16 * 1024 * 1024), // 16MB connection window
		grpc.WithDefaultCallOptions(
			grpc.MaxCallRecvMsgSize(64*1024*1024),
			grpc.MaxCallSendMsgSize(64*1024*1024),
		),
	}

	grpcOvrlyLogger.Infof("[STREAM-MGR] Starting multiplexed stream to peer %s in topo %s", s.key.peerIP, s.key.topoNs)

	for {
		select {
		case <-s.stopChan:
			grpcOvrlyLogger.Infof("[STREAM-MGR] Stopping multiplexed stream to peer %s in topo %s", s.key.peerIP, s.key.topoNs)
			return
		default:
		}

		conn, err := grpc.Dial(url, dialOpts...)
		if err != nil {
			grpcOvrlyLogger.Errorf("[STREAM-MGR] Failed to dial %s for topo %s: %v, retrying...", url, s.key.topoNs, err)
			select {
			case <-s.stopChan:
				return
			case <-time.After(2 * time.Second):
				continue
			}
		}

		ctx, cancel := context.WithCancel(context.Background())
		client := mpb.NewWireProtocolClient(conn)
		stream, err := client.SendToStream(ctx)
		if err != nil {
			cancel()
			conn.Close()
			grpcOvrlyLogger.Errorf("[STREAM-MGR] Failed to open SendToStream to %s for topo %s: %v, retrying...", url, s.key.topoNs, err)
			select {
			case <-s.stopChan:
				return
			case <-time.After(2 * time.Second):
				continue
			}
		}

		grpcOvrlyLogger.Infof("[STREAM-MGR] Successfully connected multiplexed SendToStream to %s for topo %s", url, s.key.topoNs)

		s.drainAndSend(ctx, cancel, conn, stream)

		// Throttled backoff before reconnecting to prevent tight spin loops on stream failure
		select {
		case <-s.stopChan:
			return
		case <-time.After(200 * time.Millisecond):
		}
	}
}

func (s *NodeStream) drainAndSend(ctx context.Context, cancel context.CancelFunc, conn *grpc.ClientConn, stream mpb.WireProtocol_SendToStreamClient) {
	defer func() {
		cancel()
		_ = stream.CloseSend()
		_ = conn.Close()
	}()

	for {
		select {
		case <-s.stopChan:
			return
		case pkt, ok := <-s.pktChan:
			if !ok {
				return
			}
			if err := stream.Send(pkt); err != nil {
				grpcOvrlyLogger.Debugf("[STREAM-MGR] Stream send error to %s for topo %s: %v", s.key.peerIP, s.key.topoNs, err)
				return // break out to retry/reconnect loop in run()
			}
		}
	}
}
