//go:build linux

package grpcwire

import (
	"bytes"
	"errors"
	"io"
	"os"
	"runtime/pprof"
	"sync"
	"testing"
	"time"

	mpb "github.com/openconfig/kne/third_party/meshnet/daemon/proto/meshnet/v1beta1"
	"golang.org/x/sys/unix"
)

// newNonblockingPair returns the two ends of a non-blocking SOCK_SEQPACKET
// socketpair wrapped in *os.File. Like a TAP opened with O_NONBLOCK, each
// Write is delivered as one message, and os.NewFile registers both ends with
// the Go runtime poller.
func newNonblockingPair(t *testing.T) (*os.File, *os.File) {
	t.Helper()
	fds, err := unix.Socketpair(unix.AF_UNIX, unix.SOCK_SEQPACKET|unix.SOCK_NONBLOCK|unix.SOCK_CLOEXEC, 0)
	if err != nil {
		t.Fatalf("Socketpair() failed: %v", err)
	}
	return os.NewFile(uintptr(fds[0]), "tap-side"), os.NewFile(uintptr(fds[1]), "pod-side")
}

func newTestWire(uid int64, stopC chan struct{}) *GRPCWire {
	wire := CreateGWire(int(uid), "eth1-nb", stopC, &mpb.WireDef{
		LinkUid:       uid,
		IntfNameInPod: "eth1",
		LocalPodName:  "podNB",
		LocalPodNetNs: "nsNB",
		PeerNodeIp:    "1.2.3.6",
	})
	wire.WireIfaceIDOnPeerNode = 44
	wire.IsReady = true
	return wire
}

func (m *mockPacketSender) frames() [][]byte {
	m.mu.Lock()
	defer m.mu.Unlock()
	return append([][]byte(nil), m.receivedFrames...)
}

func waitForFrames(t *testing.T, m *mockPacketSender, want int, timeout time.Duration) [][]byte {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for {
		got := m.frames()
		if len(got) >= want || time.Now().After(deadline) {
			return got
		}
		time.Sleep(10 * time.Millisecond)
	}
}

// TestForwardPackets_NonBlockingFd checks forwardPackets over a non-blocking
// fd: an idle reader (EAGAIN) is not reported as an error, frames are
// forwarded in order, and closing StopC stops forwardPackets and closes the fd.
func TestForwardPackets_NonBlockingFd(t *testing.T) {
	InitLogger()

	tapSide, podSide := newNonblockingPair(t)
	defer func() { _ = podSide.Close() }()

	sender := &mockPacketSender{}
	stopC := make(chan struct{})
	wire := newTestWire(10, stopC)

	errCh := make(chan error, 1)
	go func() { errCh <- forwardPackets(tapSide, sender, wire, "eth1-nb") }()

	// Nothing to read: every read attempt hits EAGAIN, which the poller must
	// absorb instead of returning it.
	select {
	case err := <-errCh:
		t.Fatalf("forwardPackets returned while idle: %v", err)
	case <-time.After(100 * time.Millisecond):
	}

	const numFrames = 50
	var want [][]byte
	for i := 0; i < numFrames; i++ {
		frame := bytes.Repeat([]byte{byte(i + 1)}, 1500)
		want = append(want, frame)
		if _, err := podSide.Write(frame); err != nil {
			t.Fatalf("Write(frame %d) failed: %v", i, err)
		}
	}
	got := waitForFrames(t, sender, numFrames, 2*time.Second)
	if len(got) != numFrames {
		t.Fatalf("forwarded %d frames, want %d", len(got), numFrames)
	}
	for i := range want {
		if !bytes.Equal(got[i], want[i]) {
			t.Errorf("frame %d: content mismatch", i)
		}
	}

	close(stopC)
	select {
	case err := <-errCh:
		if err != nil && !errors.Is(err, io.EOF) {
			t.Fatalf("forwardPackets() = %v, want nil or io.EOF", err)
		}
	case <-time.After(time.Second):
		t.Fatal("forwardPackets did not return after StopC was closed")
	}
	// forwardPackets closes the reader on StopC, which also unblocks the
	// goroutine parked in Read.
	if _, err := tapSide.Read(make([]byte, 1)); !errors.Is(err, os.ErrClosed) {
		t.Errorf("Read() after StopC = %v, want %v", err, os.ErrClosed)
	}
}

// TestForwardPackets_NonBlockingFdFullQueue fills the socket queue so that a
// non-blocking Write hits EAGAIN, as meshnetd's writes into a TAP can. The
// writer must park, not fail, and resume once forwardPackets drains the queue.
func TestForwardPackets_NonBlockingFdFullQueue(t *testing.T) {
	InitLogger()

	tapSide, podSide := newNonblockingPair(t)
	defer func() { _ = podSide.Close() }()

	// Many more bytes than the default socket buffer, so the queue fills.
	const numFrames = 2000
	frame := func(i int) []byte {
		b := bytes.Repeat([]byte{byte(i)}, 1500)
		b[0], b[1] = byte(i>>8), byte(i)
		return b
	}

	var wg sync.WaitGroup
	writeErr := make(chan error, 1)
	wg.Add(1)
	go func() {
		defer wg.Done()
		for i := 0; i < numFrames; i++ {
			if _, err := podSide.Write(frame(i)); err != nil {
				writeErr <- err
				return
			}
		}
	}()

	// Nobody is reading yet: the writer must be parked on a full queue, not
	// failing with EAGAIN.
	time.Sleep(200 * time.Millisecond)
	select {
	case err := <-writeErr:
		t.Fatalf("Write on full queue failed: %v", err)
	default:
	}

	sender := &mockPacketSender{}
	stopC := make(chan struct{})
	wire := newTestWire(11, stopC)
	errCh := make(chan error, 1)
	go func() { errCh <- forwardPackets(tapSide, sender, wire, "eth1-nb") }()

	wg.Wait()
	select {
	case err := <-writeErr:
		t.Fatalf("Write failed: %v", err)
	default:
	}
	got := waitForFrames(t, sender, numFrames, 5*time.Second)
	if len(got) != numFrames {
		t.Fatalf("forwarded %d frames, want %d", len(got), numFrames)
	}
	for i := range got {
		if !bytes.Equal(got[i], frame(i)) {
			t.Fatalf("frame %d: content mismatch", i)
		}
	}

	close(stopC)
	select {
	case err := <-errCh:
		if err != nil && !errors.Is(err, io.EOF) {
			t.Fatalf("forwardPackets() = %v, want nil or io.EOF", err)
		}
	case <-time.After(time.Second):
		t.Fatal("forwardPackets did not return after StopC was closed")
	}
}

// TestForwardPackets_NonBlockingFdNoThreadPerWire checks that idle wires
// backed by non-blocking fds do not each hold an OS thread (b/587062472).
func TestForwardPackets_NonBlockingFdNoThreadPerWire(t *testing.T) {
	InitLogger()

	const numWires = 200
	threads := pprof.Lookup("threadcreate")
	before := threads.Count()

	type wireRun struct {
		stopC chan struct{}
		errCh chan error
		peer  *os.File
	}
	var runs []wireRun
	for i := 0; i < numWires; i++ {
		tapSide, podSide := newNonblockingPair(t)
		stopC := make(chan struct{})
		errCh := make(chan error, 1)
		wire := newTestWire(int64(100+i), stopC)
		go func() { errCh <- forwardPackets(tapSide, &mockPacketSender{}, wire, "eth1-nb") }()
		runs = append(runs, wireRun{stopC: stopC, errCh: errCh, peer: podSide})
	}
	// Give every reader time to reach Read on its idle fd.
	time.Sleep(500 * time.Millisecond)

	if created := threads.Count() - before; created >= numWires/4 {
		t.Errorf("%d idle wires created %d OS threads, want < %d", numWires, created, numWires/4)
	}

	for _, r := range runs {
		close(r.stopC)
	}
	for i, r := range runs {
		select {
		case err := <-r.errCh:
			if err != nil && !errors.Is(err, io.EOF) {
				t.Errorf("wire %d: forwardPackets() = %v, want nil or io.EOF", i, err)
			}
		case <-time.After(2 * time.Second):
			t.Fatalf("wire %d: forwardPackets did not return after StopC was closed", i)
		}
		_ = r.peer.Close()
	}
}
