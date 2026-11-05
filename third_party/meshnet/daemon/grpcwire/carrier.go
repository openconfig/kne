package grpcwire

// This file gives grpc-wires the carrier semantics of a veth pair: an end of
// a wire has carrier (IFF_LOWER_UP) only while the other end is
// administratively up (IFF_UP). Same-node links are veth pairs and get this
// from the kernel. A grpc-wire is a TAP device at each end, and a TAP device
// has carrier whenever its file descriptor is attached, so the daemon lifts
// the admin state of each end to the peer daemon, which applies it to its own
// TAP with TUNSETCARRIER.
//
// Each daemon watches the admin state of the pod-side TAP interfaces of its
// wires, one netlink subscription per pod network namespace. Each wire has a
// carrierPusher that sends the latest admin state of its end to the peer daemon
// with Remote.SetWireCarrier. Pushes for a wire are sent one at a time and
// coalesced, so the peer always ends with the latest state. Until the peer
// first reports a state, a TAP keeps the carrier it gets on attach (up).

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/status"

	mpb "github.com/openconfig/kne/third_party/meshnet/daemon/proto/meshnet/v1beta1"
	"github.com/openconfig/kne/third_party/meshnet/utils/wireutil"
)

// Hooks replaced by tests.
var (
	// sendWireCarrier sends req to the daemon at peerIP and reports whether
	// the peer applied it.
	sendWireCarrier = grpcSendWireCarrier
	// setTAPCarrier sets the carrier of a TAP device.
	setTAPCarrier = wireutil.SetTAPCarrier
	// watchAdminState watches the admin state of the interfaces in a netns.
	watchAdminState = wireutil.WatchAdminState
)

const (
	carrierRPCTimeout = 5 * time.Second
)

var (
	carrierInitialBackoff = 100 * time.Millisecond
	carrierMaxBackoff     = 5 * time.Second
)

// carrierPusher pushes the admin state of the local end of a wire to the peer
// daemon.
type carrierPusher struct {
	wire   *GRPCWire
	notify chan struct{} // capacity 1; signals that state may need sending

	mu          sync.Mutex
	known       bool // adminUp has been observed
	adminUp     bool
	dirty       bool // the peer may not have the latest state
	requestSync bool // ask the peer to send its state back

	stopOnce sync.Once
	stop     chan struct{}
	done     chan struct{}
}

func newCarrierPusher(wire *GRPCWire) *carrierPusher {
	return &carrierPusher{
		wire:   wire,
		notify: make(chan struct{}, 1),
		stop:   make(chan struct{}),
		done:   make(chan struct{}),
	}
}

func (p *carrierPusher) kick() {
	select {
	case p.notify <- struct{}{}:
	default:
	}
}

// setAdmin records the admin state of the local end.
func (p *carrierPusher) setAdmin(up bool) {
	p.mu.Lock()
	changed := !p.known || p.adminUp != up
	p.known = true
	p.adminUp = up
	if changed {
		p.dirty = true
	}
	p.mu.Unlock()
	if changed {
		p.kick()
	}
}

// resend sends the current admin state again, for example because the peer
// re-created its end and lost it. If requestSync is set, the peer is also asked
// to send its state back.
func (p *carrierPusher) resend(requestSync bool) {
	p.mu.Lock()
	p.dirty = true
	if requestSync {
		p.requestSync = true
	}
	p.mu.Unlock()
	p.kick()
}

func (p *carrierPusher) close() {
	p.stopOnce.Do(func() { close(p.stop) })
}

// next returns the request to send, or false if there is nothing to send yet.
// It clears the pending state; a failed send restores it with retry.
func (p *carrierPusher) next() (*mpb.WireCarrier, string, bool) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if !p.dirty || !p.known {
		return nil, "", false
	}
	w := p.wire
	w.mu.Lock()
	ready := w.IsReady && w.WireIfaceIDOnPeerNode > 0 && w.PeerNodeIP != ""
	req := &mpb.WireCarrier{
		WireIfId:    w.WireIfaceIDOnPeerNode,
		LinkUid:     int64(w.UID),
		TopoNs:      w.TopoNamespace,
		PeerAdminUp: p.adminUp,
		RequestSync: p.requestSync,
	}
	peerIP := w.PeerNodeIP
	w.mu.Unlock()
	if !ready {
		return nil, "", true // retry once the wire is ready
	}
	p.dirty = false
	p.requestSync = false
	return req, peerIP, true
}

// retry restores the pending state of a request that was not applied.
func (p *carrierPusher) retry(req *mpb.WireCarrier) {
	p.mu.Lock()
	p.dirty = true
	if req != nil && req.RequestSync {
		p.requestSync = true
	}
	p.mu.Unlock()
}

func (p *carrierPusher) run() {
	defer close(p.done)
	w := p.wire
	backoff := carrierInitialBackoff
	for {
		select {
		case <-p.stop:
			return
		case <-w.StopC:
			return
		case <-p.notify:
		}
		for {
			req, peerIP, pending := p.next()
			if !pending {
				backoff = carrierInitialBackoff
				break
			}
			if req != nil {
				ctx, cancel := context.WithTimeout(context.Background(), carrierRPCTimeout)
				applied, err := sendWireCarrier(ctx, peerIP, req)
				cancel()
				if status.Code(err) == codes.Unimplemented {
					grpcOvrlyLogger.Warnf("[CARRIER] peer %s does not support SetWireCarrier; carrier of link %d will not follow %s@%s",
						peerIP, req.LinkUid, w.LocalPodName, w.LocalPodIfaceName)
					return
				}
				if err == nil && applied {
					grpcOvrlyLogger.Infof("[CARRIER] sent admin up=%t of %s@%s (link %d) to peer %s",
						req.PeerAdminUp, w.LocalPodName, w.LocalPodIfaceName, req.LinkUid, peerIP)
					backoff = carrierInitialBackoff
					continue
				}
				grpcOvrlyLogger.Debugf("[CARRIER] peer %s did not apply admin up=%t for link %d (err: %v), retrying in %v",
					peerIP, req.PeerAdminUp, req.LinkUid, err, backoff)
				p.retry(req)
			}
			select {
			case <-p.stop:
				return
			case <-w.StopC:
				return
			case <-time.After(backoff):
			}
			backoff *= 2
			if backoff > carrierMaxBackoff {
				backoff = carrierMaxBackoff
			}
		}
	}
}

// nsWatch is the admin-state watch of one pod network namespace.
type nsWatch struct {
	stop    func()
	wires   map[*GRPCWire]struct{}
	adminUp map[string]bool // last observed admin state by interface name
}

// carrierRegistry tracks the wires whose admin state is being watched.
type carrierRegistry struct {
	mu      sync.Mutex
	watches map[string]*nsWatch // by netns path
}

var carriers = &carrierRegistry{watches: map[string]*nsWatch{}}

// onAdminChange is called by the watch of netNS.
func (r *carrierRegistry) onAdminChange(netNS, ifName string, adminUp bool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	nw, ok := r.watches[netNS]
	if !ok {
		return
	}
	nw.adminUp[ifName] = adminUp
	for w := range nw.wires {
		if w.LocalPodIfaceName == ifName {
			if p := w.carrierPusher(); p != nil {
				p.setAdmin(adminUp)
			}
		}
	}
}

func (r *carrierRegistry) register(w *GRPCWire, p *carrierPusher) error {
	netNS := w.LocalPodNetNS
	r.mu.Lock()
	if nw, ok := r.watches[netNS]; ok {
		nw.wires[w] = struct{}{}
		up, known := nw.adminUp[w.LocalPodIfaceName]
		r.mu.Unlock()
		if known {
			p.setAdmin(up)
		}
		return nil
	}
	nw := &nsWatch{
		wires:   map[*GRPCWire]struct{}{w: {}},
		adminUp: map[string]bool{},
	}
	r.watches[netNS] = nw
	r.mu.Unlock()

	// The watch reports existing interfaces from its own goroutine, which
	// calls onAdminChange and so needs r.mu; do not hold it here.
	stop, err := watchAdminState(netNS, func(ifName string, adminUp bool) {
		r.onAdminChange(netNS, ifName, adminUp)
	})

	r.mu.Lock()
	defer r.mu.Unlock()
	if err != nil {
		if r.watches[netNS] == nw {
			delete(r.watches, netNS)
		}
		return err
	}
	if r.watches[netNS] != nw || len(nw.wires) == 0 {
		// Every wire was unregistered while the watch was starting.
		go stop()
		return nil
	}
	nw.stop = stop
	return nil
}

func (r *carrierRegistry) unregister(w *GRPCWire) {
	r.mu.Lock()
	defer r.mu.Unlock()
	nw, ok := r.watches[w.LocalPodNetNS]
	if !ok {
		return
	}
	if _, ok := nw.wires[w]; !ok {
		return
	}
	delete(nw.wires, w)
	if len(nw.wires) > 0 {
		return
	}
	delete(r.watches, w.LocalPodNetNS)
	if nw.stop != nil {
		// stop waits for the watch goroutine, which may be waiting for
		// r.mu in onAdminChange.
		go nw.stop()
	}
}

// carrierPusher returns the wire's carrier pusher, or nil if it has none.
func (wire *GRPCWire) carrierPusher() *carrierPusher {
	wire.mu.Lock()
	defer wire.mu.Unlock()
	return wire.carrier
}

// startCarrier starts lifting the admin state of the wire's local end to the
// peer. If requestSync is set, the peer is asked to send its state back.
func startCarrier(wire *GRPCWire, requestSync bool) {
	if wire == nil {
		return
	}
	wire.mu.Lock()
	if wire.carrier != nil {
		wire.mu.Unlock()
		return
	}
	p := newCarrierPusher(wire)
	wire.carrier = p
	wire.mu.Unlock()

	go p.run()
	if err := carriers.register(wire, p); err != nil {
		grpcOvrlyLogger.Warnf("[CARRIER] cannot watch admin state of %s@%s in %s; peer carrier will not follow it: %v",
			wire.LocalPodName, wire.LocalPodIfaceName, wire.LocalPodNetNS, err)
	}
	// Send the current state once the wire is ready, and ask for the
	// peer's state if needed.
	p.resend(requestSync)
}

// stopCarrier stops lifting the admin state of the wire. It is idempotent and
// does not take wires.mu, so it may be called with it held.
func stopCarrier(wire *GRPCWire) {
	if wire == nil {
		return
	}
	carriers.unregister(wire)
	if p := wire.carrierPusher(); p != nil {
		p.close()
	}
}

// SetWireCarrier applies the admin state of the peer's end of a wire as the
// carrier of the local end. It reports whether the wire was found.
func SetWireCarrier(req *mpb.WireCarrier) (bool, error) {
	wires.mu.Lock()
	var wire *GRPCWire
	for _, w := range wires.wires {
		if w.LocalNodeIfaceID == req.GetWireIfId() {
			wire = w
			break
		}
	}
	handle := wires.handles[req.GetWireIfId()]
	wires.mu.Unlock()

	if wire == nil || handle == nil {
		grpcOvrlyLogger.Debugf("[CARRIER] SetWireCarrier: no local wire with id %d (link %d)", req.GetWireIfId(), req.GetLinkUid())
		return false, nil
	}
	if int64(wire.UID) != req.GetLinkUid() || (req.GetTopoNs() != "" && wire.TopoNamespace != "" && wire.TopoNamespace != req.GetTopoNs()) {
		grpcOvrlyLogger.Warnf("[CARRIER] SetWireCarrier: wire id %d belongs to link %d in %q, not link %d in %q",
			req.GetWireIfId(), wire.UID, wire.TopoNamespace, req.GetLinkUid(), req.GetTopoNs())
		return false, nil
	}
	if err := setTAPCarrier(handle, req.GetPeerAdminUp()); err != nil {
		return false, fmt.Errorf("setting carrier of %s@%s: %w", wire.LocalPodName, wire.LocalPodIfaceName, err)
	}
	grpcOvrlyLogger.Infof("[CARRIER] set carrier of %s@%s (link %d) to %t", wire.LocalPodName, wire.LocalPodIfaceName, wire.UID, req.GetPeerAdminUp())
	if req.GetRequestSync() {
		if p := wire.carrierPusher(); p != nil {
			p.resend(false)
		}
	}
	return true, nil
}

// remoteConns caches client connections to peer daemons by address.
var remoteConns = struct {
	mu    sync.Mutex
	conns map[string]*grpc.ClientConn
}{conns: map[string]*grpc.ClientConn{}}

func remoteConn(peerIP string) (*grpc.ClientConn, error) {
	url := strings.TrimSpace(fmt.Sprintf("%s:%d", peerIP, wireutil.GRPCDefaultPort))
	remoteConns.mu.Lock()
	defer remoteConns.mu.Unlock()
	if c, ok := remoteConns.conns[url]; ok {
		return c, nil
	}
	c, err := grpc.NewClient(url, grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		return nil, err
	}
	remoteConns.conns[url] = c
	return c, nil
}

func grpcSendWireCarrier(ctx context.Context, peerIP string, req *mpb.WireCarrier) (bool, error) {
	conn, err := remoteConn(peerIP)
	if err != nil {
		return false, err
	}
	resp, err := mpb.NewRemoteClient(conn).SetWireCarrier(ctx, req)
	if err != nil {
		return false, err
	}
	return resp.GetResponse(), nil
}
