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
	"errors"
	"net"
	"sync"
	"testing"
	"time"

	"golang.org/x/sys/unix"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/test/bufconn"

	wpb "github.com/openconfig/kne/proto/wire"
)

// fakeLink is a Link that behaves like one end of a veth pair: its carrier
// follows the admin state of the far end, which tests set with setPeerUp.
type fakeLink struct {
	mu        sync.Mutex
	obs       LinkObservation
	events    chan LinkObservation
	sets      chan bool
	closed    bool
	closeOnce sync.Once
}

func newFakeLink(adminUp, carrier bool) *fakeLink {
	return &fakeLink{
		obs:    LinkObservation{AdminUp: adminUp, Carrier: carrier},
		events: make(chan LinkObservation, 1),
		sets:   make(chan bool, 100),
	}
}

func (f *fakeLink) publishLocked() {
	if !f.closed {
		publishLatest(f.events, f.obs)
	}
}

// SetAdminUp implements Link.
func (f *fakeLink) SetAdminUp(up bool) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.sets <- up
	if f.obs.AdminUp != up {
		f.obs.AdminUp = up
		f.publishLocked()
	}
	return nil
}

// Current implements Link.
func (f *fakeLink) Current() (LinkObservation, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.obs, nil
}

// Events implements Link.
func (f *fakeLink) Events() <-chan LinkObservation {
	return f.events
}

// Close implements Link.
func (f *fakeLink) Close() error {
	f.closeOnce.Do(func() {
		f.mu.Lock()
		defer f.mu.Unlock()
		f.closed = true
		close(f.events)
	})
	return nil
}

// setPeerUp simulates the far device going administratively up or down.
func (f *fakeLink) setPeerUp(up bool) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.obs.Carrier = up
	f.publishLocked()
}

func (f *fakeLink) nextSet(t *testing.T) bool {
	t.Helper()
	select {
	case up := <-f.sets:
		return up
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for SetAdminUp")
		return false
	}
}

func (f *fakeLink) noSet(t *testing.T) {
	t.Helper()
	select {
	case up := <-f.sets:
		t.Fatalf("unexpected SetAdminUp(%t)", up)
	case <-time.After(100 * time.Millisecond):
	}
}

func (f *fakeLink) isClosed() bool {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.closed
}

// noLink is a link opener for tests that do not exercise link state.
func noLink(string) (Link, error) {
	return nil, errors.New("link state disabled in test")
}

func TestLinkReporter(t *testing.T) {
	const (
		unknown = wpb.LinkState_LINK_STATE_UNKNOWN
		up      = wpb.LinkState_LINK_STATE_UP
		down    = wpb.LinkState_LINK_STATE_DOWN
	)
	type step struct {
		obs  LinkObservation
		want wpb.LinkState
	}
	tests := []struct {
		desc  string
		steps []step
	}{{
		desc: "carrier changes are reported once each",
		steps: []step{
			{LinkObservation{AdminUp: true, Carrier: true}, up},
			{LinkObservation{AdminUp: true, Carrier: true}, unknown},
			{LinkObservation{AdminUp: true, Carrier: false}, down},
			{LinkObservation{AdminUp: true, Carrier: false}, unknown},
			{LinkObservation{AdminUp: true, Carrier: true}, up},
		},
	}, {
		desc: "nothing is reported while admin down",
		steps: []step{
			{LinkObservation{AdminUp: true, Carrier: true}, up},
			{LinkObservation{AdminUp: false, Carrier: false}, unknown},
			{LinkObservation{AdminUp: false, Carrier: true}, unknown},
		},
	}, {
		desc: "the first observation after admin up is reported",
		steps: []step{
			{LinkObservation{AdminUp: true, Carrier: true}, up},
			{LinkObservation{AdminUp: false, Carrier: false}, unknown},
			{LinkObservation{AdminUp: true, Carrier: true}, up},
		},
	}, {
		desc: "admin up without carrier reports down",
		steps: []step{
			{LinkObservation{AdminUp: false}, unknown},
			{LinkObservation{AdminUp: true, Carrier: false}, down},
		},
	}}
	for _, tt := range tests {
		t.Run(tt.desc, func(t *testing.T) {
			var r linkReporter
			for i, s := range tt.steps {
				if got := r.observe(s.obs); got != s.want {
					t.Errorf("step %d: observe(%+v) = %v, want %v", i, s.obs, got, s.want)
				}
			}
		})
	}
}

func TestPublishLatestKeepsNewest(t *testing.T) {
	ch := make(chan int, 1)
	for i := 1; i <= 5; i++ {
		publishLatest(ch, i)
	}
	if got := <-ch; got != 5 {
		t.Errorf("publishLatest kept %d, want 5", got)
	}
}

// linkTestEnv is a bridge Server with a fake interface and link, served over
// bufconn.
type linkTestEnv struct {
	ctx    context.Context
	server *Server
	io     ReadWriter
	link   *fakeLink
	client wpb.WireClient
}

func newLinkTestEnv(t *testing.T, link *fakeLink, rw ReadWriter) *linkTestEnv {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	t.Cleanup(cancel)

	server := NewServer(ctx)
	t.Cleanup(func() { _ = server.Close() })
	server.SetSocketOpener(func(string) (ReadWriter, error) { return rw, nil })
	if link != nil {
		server.SetLinkOpener(func(string) (Link, error) { return link, nil })
	} else {
		server.SetLinkOpener(nil)
	}

	lis := bufconn.Listen(1024 * 1024)
	grpcServer := grpc.NewServer()
	wpb.RegisterWireServer(grpcServer, server)
	go func() { _ = grpcServer.Serve(lis) }()
	t.Cleanup(grpcServer.Stop)

	conn, err := grpc.NewClient("passthrough://bufnet",
		grpc.WithContextDialer(func(context.Context, string) (net.Conn, error) { return lis.Dial() }),
		grpc.WithTransportCredentials(insecure.NewCredentials()),
	)
	if err != nil {
		t.Fatalf("Failed to dial bufnet: %v", err)
	}
	t.Cleanup(func() { _ = conn.Close() })
	return &linkTestEnv{ctx: ctx, server: server, io: rw, link: link, client: wpb.NewWireClient(conn)}
}

// testStream is a Transmit stream whose received messages are collected in msgs.
type testStream struct {
	stream wpb.Wire_TransmitClient
	msgs   chan *wpb.Packet
}

func (e *linkTestEnv) open(t *testing.T) *testStream {
	t.Helper()
	ctx := metadata.NewOutgoingContext(e.ctx, metadata.Pairs("interface", "eth1"))
	stream, err := e.client.Transmit(ctx)
	if err != nil {
		t.Fatalf("Transmit RPC failed: %v", err)
	}
	if _, err := stream.Header(); err != nil {
		t.Fatalf("Failed to receive stream header: %v", err)
	}
	ts := &testStream{stream: stream, msgs: make(chan *wpb.Packet, 100)}
	go func() {
		defer close(ts.msgs)
		for {
			m, err := stream.Recv()
			if err != nil {
				return
			}
			ts.msgs <- m
		}
	}()
	return ts
}

func (ts *testStream) send(t *testing.T, p *wpb.Packet) {
	t.Helper()
	if err := ts.stream.Send(p); err != nil {
		t.Fatalf("Send(%v) failed: %v", p, err)
	}
}

func (ts *testStream) next(t *testing.T) *wpb.Packet {
	t.Helper()
	select {
	case m, ok := <-ts.msgs:
		if !ok {
			t.Fatal("stream ended")
		}
		return m
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for a message")
		return nil
	}
}

func (ts *testStream) nextLinkState(t *testing.T) wpb.LinkState {
	t.Helper()
	m := ts.next(t)
	if len(m.GetData()) != 0 {
		t.Fatalf("got frame %v, want a link-state message", m.GetData())
	}
	return m.GetLinkState()
}

func (ts *testStream) none(t *testing.T) {
	t.Helper()
	select {
	case m, ok := <-ts.msgs:
		if ok {
			t.Fatalf("unexpected message: %v", m)
		}
		t.Fatal("stream ended")
	case <-time.After(150 * time.Millisecond):
	}
}

func TestTransmitSendsInitialLinkState(t *testing.T) {
	tests := []struct {
		desc string
		link *fakeLink
		want wpb.LinkState
	}{
		{"carrier", newFakeLink(true, true), wpb.LinkState_LINK_STATE_UP},
		{"no carrier", newFakeLink(true, false), wpb.LinkState_LINK_STATE_DOWN},
	}
	for _, tt := range tests {
		t.Run(tt.desc, func(t *testing.T) {
			env := newLinkTestEnv(t, tt.link, newFakeReadWriter())
			ts := env.open(t)
			if got := ts.nextLinkState(t); got != tt.want {
				t.Errorf("initial link state = %v, want %v", got, tt.want)
			}
			ts.none(t)
		})
	}
}

func TestTransmitNoInitialLinkStateWhileAdminDown(t *testing.T) {
	env := newLinkTestEnv(t, newFakeLink(false, false), newFakeReadWriter())
	ts := env.open(t)
	ts.none(t)
}

func TestTransmitReportsCarrierToAllClients(t *testing.T) {
	link := newFakeLink(true, true)
	env := newLinkTestEnv(t, link, newFakeReadWriter())
	ts1 := env.open(t)
	ts2 := env.open(t)
	for _, ts := range []*testStream{ts1, ts2} {
		if got := ts.nextLinkState(t); got != wpb.LinkState_LINK_STATE_UP {
			t.Fatalf("initial link state = %v, want UP", got)
		}
	}

	link.setPeerUp(false)
	for i, ts := range []*testStream{ts1, ts2} {
		if got := ts.nextLinkState(t); got != wpb.LinkState_LINK_STATE_DOWN {
			t.Errorf("client %d: link state after peer down = %v, want DOWN", i+1, got)
		}
	}
	link.setPeerUp(true)
	for i, ts := range []*testStream{ts1, ts2} {
		if got := ts.nextLinkState(t); got != wpb.LinkState_LINK_STATE_UP {
			t.Errorf("client %d: link state after peer up = %v, want UP", i+1, got)
		}
	}
}

func TestTransmitAppliesReceivedLinkState(t *testing.T) {
	link := newFakeLink(true, true)
	fakeIO := newFakeReadWriter()
	env := newLinkTestEnv(t, link, fakeIO)
	ts := env.open(t)
	ts.nextLinkState(t)

	// A link-state-only message changes the admin state and writes nothing.
	ts.send(t, &wpb.Packet{LinkState: wpb.LinkState_LINK_STATE_DOWN})
	if up := link.nextSet(t); up {
		t.Errorf("SetAdminUp(%t) for DOWN, want false", up)
	}
	select {
	case pkt := <-fakeIO.writeChan:
		t.Errorf("link-state message wrote frame %v", pkt)
	default:
	}

	// A frame-only message writes the frame and leaves the admin state alone.
	frame := []byte{0x01, 0x02}
	ts.send(t, &wpb.Packet{Data: frame})
	select {
	case pkt := <-fakeIO.writeChan:
		if !bytes.Equal(pkt, frame) {
			t.Errorf("wrote %v, want %v", pkt, frame)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for frame")
	}
	link.noSet(t)

	// A message can carry both.
	ts.send(t, &wpb.Packet{Data: frame, LinkState: wpb.LinkState_LINK_STATE_UP})
	if up := link.nextSet(t); !up {
		t.Errorf("SetAdminUp(%t) for UP, want true", up)
	}
	<-fakeIO.writeChan
}

func TestTransmitSuppressesSelfInducedCarrierLoss(t *testing.T) {
	link := newFakeLink(true, true)
	env := newLinkTestEnv(t, link, newFakeReadWriter())
	ts := env.open(t)
	ts.nextLinkState(t)

	// The peer propagates DOWN: the interface goes admin down. Neither that
	// nor any carrier change while it is down is reported back.
	ts.send(t, &wpb.Packet{LinkState: wpb.LinkState_LINK_STATE_DOWN})
	link.nextSet(t)
	link.setPeerUp(false)
	link.setPeerUp(true)
	ts.none(t)

	// When the peer propagates UP, the current carrier is reported right away.
	ts.send(t, &wpb.Packet{LinkState: wpb.LinkState_LINK_STATE_UP})
	link.nextSet(t)
	if got := ts.nextLinkState(t); got != wpb.LinkState_LINK_STATE_UP {
		t.Errorf("link state after admin up = %v, want UP", got)
	}
}

func TestTransmitWithoutLinkState(t *testing.T) {
	fakeIO := newFakeReadWriter()
	env := newLinkTestEnv(t, nil, fakeIO)
	ts := env.open(t)
	ts.none(t)

	// Link state is ignored and frames still flow.
	frame := []byte{0x01}
	ts.send(t, &wpb.Packet{Data: frame, LinkState: wpb.LinkState_LINK_STATE_DOWN})
	select {
	case pkt := <-fakeIO.writeChan:
		if !bytes.Equal(pkt, frame) {
			t.Errorf("wrote %v, want %v", pkt, frame)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for frame")
	}
}

func TestTransmitLinkClosedWithDemux(t *testing.T) {
	link := newFakeLink(true, true)
	fakeIO := newFakeReadWriter()
	env := newLinkTestEnv(t, link, fakeIO)
	ts := env.open(t)
	ts.nextLinkState(t)

	_ = fakeIO.Close()
	deadline := time.Now().Add(2 * time.Second)
	for !link.isClosed() {
		if time.Now().After(deadline) {
			t.Fatal("link not closed after the demux terminated")
		}
		time.Sleep(time.Millisecond)
	}
}

// enetdownReader returns ENETDOWN from its first read, as an AF_PACKET socket
// does when its interface goes down.
type enetdownReader struct {
	*fakeReadWriter
	once sync.Once
}

func (r *enetdownReader) ReadPacket() ([]byte, error) {
	var first bool
	r.once.Do(func() { first = true })
	if first {
		return nil, unix.ENETDOWN
	}
	return r.fakeReadWriter.ReadPacket()
}

func TestTransmitSurvivesENETDOWN(t *testing.T) {
	fakeIO := newFakeReadWriter()
	env := newLinkTestEnv(t, nil, &enetdownReader{fakeReadWriter: fakeIO})
	ts := env.open(t)

	// Reading resumes after ENETDOWN.
	frame := []byte{0x0a, 0x0b}
	fakeIO.sendPacket(frame)
	if m := ts.next(t); !bytes.Equal(m.GetData(), frame) {
		t.Errorf("received %v, want %v", m.GetData(), frame)
	}

	// A write that fails with ENETDOWN drops the frame but keeps the stream.
	fakeIO.mu.Lock()
	fakeIO.writeErr = unix.ENETDOWN
	fakeIO.mu.Unlock()
	ts.send(t, &wpb.Packet{Data: []byte{0x01}})
	time.Sleep(50 * time.Millisecond)
	fakeIO.mu.Lock()
	fakeIO.writeErr = nil
	fakeIO.mu.Unlock()
	ts.send(t, &wpb.Packet{Data: frame})
	select {
	case pkt := <-fakeIO.writeChan:
		if !bytes.Equal(pkt, frame) {
			t.Errorf("wrote %v, want %v", pkt, frame)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for frame after ENETDOWN")
	}
}
