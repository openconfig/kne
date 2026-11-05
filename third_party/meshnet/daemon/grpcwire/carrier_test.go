package grpcwire

import (
	"context"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	mpb "github.com/openconfig/kne/third_party/meshnet/daemon/proto/meshnet/v1beta1"
	"github.com/openconfig/kne/third_party/meshnet/utils/wireutil"
)

// fakeSender records SetWireCarrier requests and answers them with reply.
type fakeSender struct {
	mu    sync.Mutex
	reqs  chan *mpb.WireCarrier
	reply func(*mpb.WireCarrier) (bool, error)
}

func newFakeSender() *fakeSender {
	return &fakeSender{
		reqs:  make(chan *mpb.WireCarrier, 100),
		reply: func(*mpb.WireCarrier) (bool, error) { return true, nil },
	}
}

func (f *fakeSender) setReply(reply func(*mpb.WireCarrier) (bool, error)) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.reply = reply
}

func (f *fakeSender) send(_ context.Context, _ string, req *mpb.WireCarrier) (bool, error) {
	f.mu.Lock()
	reply := f.reply
	f.mu.Unlock()
	f.reqs <- req
	return reply(req)
}

// next returns the next request, failing the test after a timeout.
func (f *fakeSender) next(t *testing.T) *mpb.WireCarrier {
	t.Helper()
	select {
	case r := <-f.reqs:
		return r
	case <-time.After(5 * time.Second):
		t.Fatal("timed out waiting for SetWireCarrier")
		return nil
	}
}

// none checks that no request is sent for a short while.
func (f *fakeSender) none(t *testing.T) {
	t.Helper()
	select {
	case r := <-f.reqs:
		t.Fatalf("unexpected SetWireCarrier: %v", r)
	case <-time.After(100 * time.Millisecond):
	}
}

// fakeWatcher replaces watchAdminState.
type fakeWatcher struct {
	mu      sync.Mutex
	cbs     map[string]wireutil.AdminStateFunc
	stopped map[string]int
}

func (f *fakeWatcher) watch(nsPath string, onChange wireutil.AdminStateFunc) (func(), error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.cbs[nsPath] = onChange
	return func() {
		f.mu.Lock()
		defer f.mu.Unlock()
		f.stopped[nsPath]++
		delete(f.cbs, nsPath)
	}, nil
}

func (f *fakeWatcher) change(t *testing.T, nsPath, ifName string, up bool) {
	t.Helper()
	f.mu.Lock()
	cb := f.cbs[nsPath]
	f.mu.Unlock()
	if cb == nil {
		t.Fatalf("no watch on %s", nsPath)
	}
	cb(ifName, up)
}

func (f *fakeWatcher) stopCount(nsPath string) int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.stopped[nsPath]
}

// setupCarrierTest replaces the hooks and global state used by carrier.go.
func setupCarrierTest(t *testing.T) (*fakeSender, *fakeWatcher) {
	t.Helper()
	sender := newFakeSender()
	watcher := &fakeWatcher{cbs: map[string]wireutil.AdminStateFunc{}, stopped: map[string]int{}}

	oldSend, oldWatch, oldSet := sendWireCarrier, watchAdminState, setTAPCarrier
	oldInitial, oldMax := carrierInitialBackoff, carrierMaxBackoff
	oldWires, oldCarriers := wires, carriers
	sendWireCarrier = sender.send
	watchAdminState = watcher.watch
	carrierInitialBackoff, carrierMaxBackoff = time.Millisecond, 10*time.Millisecond
	wires = &wireMap{wires: map[linkKey]*GRPCWire{}, handles: map[int64]*os.File{}}
	carriers = &carrierRegistry{watches: map[string]*nsWatch{}}
	t.Cleanup(func() {
		sendWireCarrier, watchAdminState, setTAPCarrier = oldSend, oldWatch, oldSet
		carrierInitialBackoff, carrierMaxBackoff = oldInitial, oldMax
		wires, carriers = oldWires, oldCarriers
	})
	return sender, watcher
}

// stopAndWait stops the wire's carrier and waits for its pusher to exit, so
// that the test hooks can be restored safely.
func stopAndWait(t *testing.T, w *GRPCWire) {
	t.Helper()
	stopCarrier(w)
	if p := w.carrierPusher(); p != nil {
		select {
		case <-p.done:
		case <-time.After(5 * time.Second):
			t.Errorf("carrier pusher for link %d did not stop", w.UID)
		}
	}
}

func testWire(netNS, ifName string, uid int, localID, peerID int64) *GRPCWire {
	return &GRPCWire{
		UID:                   uid,
		TopoNamespace:         "topo",
		LocalPodName:          "pod",
		LocalPodNetNS:         netNS,
		LocalPodIfaceName:     ifName,
		LocalNodeIfaceID:      localID,
		WireIfaceIDOnPeerNode: peerID,
		PeerNodeIP:            "10.0.0.2",
		IsReady:               true,
		StopC:                 make(chan struct{}),
	}
}

func TestCarrierPushesAdminChanges(t *testing.T) {
	sender, watcher := setupCarrierTest(t)
	w := testWire("/ns/a", "eth1", 7, 10, 20)
	startCarrier(w, false)
	defer stopAndWait(t, w)

	// Nothing is sent until the admin state is known.
	sender.none(t)

	watcher.change(t, "/ns/a", "eth1", true)
	r := sender.next(t)
	if r.GetWireIfId() != 20 || r.GetLinkUid() != 7 || r.GetTopoNs() != "topo" || !r.GetPeerAdminUp() || r.GetRequestSync() {
		t.Errorf("first push = %v, want wire 20, link 7, topo, up, no sync", r)
	}

	// Other interfaces in the namespace are ignored.
	watcher.change(t, "/ns/a", "eth2", false)
	sender.none(t)

	watcher.change(t, "/ns/a", "eth1", false)
	if r := sender.next(t); r.GetPeerAdminUp() {
		t.Errorf("push after admin down = %v, want down", r)
	}
	// Repeating the same state sends nothing.
	watcher.change(t, "/ns/a", "eth1", false)
	sender.none(t)
}

func TestCarrierCoalescesToLatestState(t *testing.T) {
	sender, watcher := setupCarrierTest(t)
	block := make(chan struct{})
	sender.setReply(func(*mpb.WireCarrier) (bool, error) {
		<-block
		return true, nil
	})
	w := testWire("/ns/a", "eth1", 7, 10, 20)
	startCarrier(w, false)
	defer stopAndWait(t, w)

	watcher.change(t, "/ns/a", "eth1", true)
	sender.next(t) // in flight and blocked
	for i := 0; i < 10; i++ {
		watcher.change(t, "/ns/a", "eth1", i%2 == 0)
	}
	watcher.change(t, "/ns/a", "eth1", false)
	close(block)

	if r := sender.next(t); r.GetPeerAdminUp() {
		t.Errorf("push after burst = %v, want the latest state (down)", r)
	}
	sender.none(t)
}

func TestCarrierRetriesUntilApplied(t *testing.T) {
	sender, watcher := setupCarrierTest(t)
	var mu sync.Mutex
	calls := 0
	sender.setReply(func(*mpb.WireCarrier) (bool, error) {
		mu.Lock()
		defer mu.Unlock()
		calls++
		switch calls {
		case 1:
			return false, status.Error(codes.Unavailable, "peer down")
		case 2:
			return false, nil // peer does not know the wire yet
		}
		return true, nil
	})
	w := testWire("/ns/a", "eth1", 7, 10, 20)
	startCarrier(w, false)
	defer stopAndWait(t, w)

	watcher.change(t, "/ns/a", "eth1", true)
	for i := 0; i < 3; i++ {
		if r := sender.next(t); !r.GetPeerAdminUp() {
			t.Errorf("attempt %d = %v, want up", i+1, r)
		}
	}
	sender.none(t)
}

func TestCarrierGivesUpOnUnimplemented(t *testing.T) {
	sender, watcher := setupCarrierTest(t)
	sender.setReply(func(*mpb.WireCarrier) (bool, error) {
		return false, status.Error(codes.Unimplemented, "old peer")
	})
	w := testWire("/ns/a", "eth1", 7, 10, 20)
	startCarrier(w, false)
	defer stopAndWait(t, w)

	watcher.change(t, "/ns/a", "eth1", true)
	sender.next(t)
	watcher.change(t, "/ns/a", "eth1", false)
	sender.none(t)
}

func TestCarrierWaitsForWireReady(t *testing.T) {
	sender, watcher := setupCarrierTest(t)
	w := testWire("/ns/a", "eth1", 7, 10, 0)
	w.IsReady = false
	w.PeerNodeIP = ""
	startCarrier(w, false)
	defer stopAndWait(t, w)

	watcher.change(t, "/ns/a", "eth1", false)
	sender.none(t)

	w.UpdateWire(30, "", nil)
	w.mu.Lock()
	w.PeerNodeIP = "10.0.0.3" // UpdateWire would also start a packet stream
	w.mu.Unlock()
	r := sender.next(t)
	if r.GetWireIfId() != 30 || r.GetPeerAdminUp() {
		t.Errorf("push once ready = %v, want wire 30, down", r)
	}
}

func TestCarrierResendsOnUpdateWire(t *testing.T) {
	sender, watcher := setupCarrierTest(t)
	w := testWire("/ns/a", "eth1", 7, 10, 20)
	startCarrier(w, false)
	defer stopAndWait(t, w)

	watcher.change(t, "/ns/a", "eth1", true)
	sender.next(t)

	// The peer re-created its end with a new interface id.
	w.UpdateWire(21, "", nil)
	if r := sender.next(t); r.GetWireIfId() != 21 || !r.GetPeerAdminUp() {
		t.Errorf("push after UpdateWire = %v, want wire 21, up", r)
	}
}

func TestCarrierRequestSync(t *testing.T) {
	sender, watcher := setupCarrierTest(t)
	w := testWire("/ns/a", "eth1", 7, 10, 20)
	startCarrier(w, true)
	defer stopAndWait(t, w)

	watcher.change(t, "/ns/a", "eth1", true)
	if r := sender.next(t); !r.GetRequestSync() {
		t.Errorf("first push after recon = %v, want request_sync", r)
	}
	watcher.change(t, "/ns/a", "eth1", false)
	if r := sender.next(t); r.GetRequestSync() {
		t.Errorf("later push = %v, want no request_sync", r)
	}
}

func TestCarrierSharedNamespaceWatch(t *testing.T) {
	sender, watcher := setupCarrierTest(t)
	w1 := testWire("/ns/a", "eth1", 1, 10, 20)
	w2 := testWire("/ns/a", "eth2", 2, 11, 21)
	startCarrier(w1, false)
	watcher.change(t, "/ns/a", "eth1", true)
	watcher.change(t, "/ns/a", "eth2", false) // before w2 exists
	sender.next(t)

	// A wire registered later is seeded from the state already observed.
	startCarrier(w2, false)
	if r := sender.next(t); r.GetLinkUid() != 2 || r.GetPeerAdminUp() {
		t.Errorf("push for second wire = %v, want link 2, down", r)
	}

	stopAndWait(t, w1)
	if got := watcher.stopCount("/ns/a"); got != 0 {
		t.Errorf("watch stopped %d times with a wire still registered, want 0", got)
	}
	stopAndWait(t, w2)
	stopCarrier(w2) // idempotent
	deadline := time.Now().Add(5 * time.Second)
	for watcher.stopCount("/ns/a") != 1 {
		if time.Now().After(deadline) {
			t.Fatalf("watch stopped %d times after the last wire, want 1", watcher.stopCount("/ns/a"))
		}
		time.Sleep(time.Millisecond)
	}
}

func TestSetWireCarrier(t *testing.T) {
	sender, watcher := setupCarrierTest(t)
	type setCall struct {
		name string
		up   bool
	}
	sets := make(chan setCall, 10)
	setTAPCarrier = func(f *os.File, up bool) error {
		sets <- setCall{f.Name(), up}
		return nil
	}

	f, err := os.Create(filepath.Join(t.TempDir(), "tap"))
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = f.Close() }()
	w := testWire("/ns/a", "eth1", 7, 10, 20)
	if err := wires.AddInMem(w, f); err != nil {
		t.Fatalf("AddInMem() failed: %v", err)
	}
	startCarrier(w, false)
	defer stopAndWait(t, w)
	watcher.change(t, "/ns/a", "eth1", true)
	sender.next(t)

	tests := []struct {
		desc    string
		req     *mpb.WireCarrier
		applied bool
		wantSet bool
	}{
		{"applies down", &mpb.WireCarrier{WireIfId: 10, LinkUid: 7, TopoNs: "topo", PeerAdminUp: false}, true, true},
		{"applies up", &mpb.WireCarrier{WireIfId: 10, LinkUid: 7, TopoNs: "topo", PeerAdminUp: true}, true, true},
		{"unknown wire id", &mpb.WireCarrier{WireIfId: 99, LinkUid: 7, TopoNs: "topo"}, false, false},
		{"stale wire id", &mpb.WireCarrier{WireIfId: 10, LinkUid: 8, TopoNs: "topo"}, false, false},
		{"other topology", &mpb.WireCarrier{WireIfId: 10, LinkUid: 7, TopoNs: "other"}, false, false},
	}
	for _, tt := range tests {
		t.Run(tt.desc, func(t *testing.T) {
			applied, err := SetWireCarrier(tt.req)
			if err != nil {
				t.Fatalf("SetWireCarrier() failed: %v", err)
			}
			if applied != tt.applied {
				t.Errorf("SetWireCarrier() applied = %t, want %t", applied, tt.applied)
			}
			select {
			case s := <-sets:
				if !tt.wantSet {
					t.Errorf("unexpected carrier set: %+v", s)
				} else if s.up != tt.req.GetPeerAdminUp() {
					t.Errorf("carrier set to %t, want %t", s.up, tt.req.GetPeerAdminUp())
				}
			default:
				if tt.wantSet {
					t.Error("carrier not set")
				}
			}
		})
	}
	sender.none(t)

	// request_sync makes the receiver send its own state back.
	if _, err := SetWireCarrier(&mpb.WireCarrier{WireIfId: 10, LinkUid: 7, TopoNs: "topo", PeerAdminUp: true, RequestSync: true}); err != nil {
		t.Fatalf("SetWireCarrier(request_sync) failed: %v", err)
	}
	<-sets
	if r := sender.next(t); !r.GetPeerAdminUp() || r.GetRequestSync() {
		t.Errorf("push answering request_sync = %v, want up without request_sync", r)
	}
}
