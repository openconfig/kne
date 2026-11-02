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
	"os"
	"runtime"
	"testing"
	"time"

	"github.com/vishvananda/netlink"
	"github.com/vishvananda/netns"
)

// TestNetlinkLinkVeth checks netlinkLink against a real veth pair in a
// throwaway network namespace: the carrier of one end follows the admin state
// of the other. It needs CAP_NET_ADMIN, e.g. `sudo go test` or
// `unshare -rn` on the test binary.
func TestNetlinkLinkVeth(t *testing.T) {
	if os.Getuid() != 0 {
		t.Skip("Test requires root privileges (or run the test binary under unshare -rn)")
	}
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	orig, err := netns.Get()
	if err != nil {
		t.Fatalf("netns.Get() failed: %v", err)
	}
	defer func() { _ = orig.Close() }()
	ns, err := netns.New() // also switches this thread into it
	if err != nil {
		t.Skipf("cannot create a network namespace: %v", err)
	}
	defer func() {
		_ = netns.Set(orig)
		_ = ns.Close()
	}()

	if err := netlink.LinkAdd(&netlink.Veth{LinkAttrs: netlink.LinkAttrs{Name: "bra"}, PeerName: "brz"}); err != nil {
		t.Fatalf("creating veth pair failed: %v", err)
	}
	peer, err := netlink.LinkByName("brz")
	if err != nil {
		t.Fatalf("LinkByName(brz) failed: %v", err)
	}
	if err := netlink.LinkSetUp(peer); err != nil {
		t.Fatalf("LinkSetUp(brz) failed: %v", err)
	}

	l, err := NewNetlinkLink("bra")
	if err != nil {
		t.Fatalf("NewNetlinkLink(bra) failed: %v", err)
	}
	defer func() { _ = l.Close() }()

	waitFor := func(want LinkObservation) {
		t.Helper()
		deadline := time.After(5 * time.Second)
		for {
			if got, err := l.Current(); err == nil && got == want {
				return
			}
			select {
			case <-l.Events():
			case <-time.After(50 * time.Millisecond):
			case <-deadline:
				got, _ := l.Current()
				t.Fatalf("link state = %+v, want %+v", got, want)
			}
		}
	}

	if err := l.SetAdminUp(true); err != nil {
		t.Fatalf("SetAdminUp(true) failed: %v", err)
	}
	waitFor(LinkObservation{AdminUp: true, Carrier: true})

	// The far end going down removes carrier, and an event is delivered.
	if err := netlink.LinkSetDown(peer); err != nil {
		t.Fatalf("LinkSetDown(brz) failed: %v", err)
	}
	select {
	case obs := <-l.Events():
		_ = obs // coalesced; checked below
	case <-time.After(5 * time.Second):
		t.Fatal("no link event after the far end went down")
	}
	waitFor(LinkObservation{AdminUp: true, Carrier: false})

	if err := netlink.LinkSetUp(peer); err != nil {
		t.Fatalf("LinkSetUp(brz) failed: %v", err)
	}
	waitFor(LinkObservation{AdminUp: true, Carrier: true})

	// Setting this end down is seen by the far end as loss of carrier.
	if err := l.SetAdminUp(false); err != nil {
		t.Fatalf("SetAdminUp(false) failed: %v", err)
	}
	waitFor(LinkObservation{AdminUp: false, Carrier: false})
	if p, err := netlink.LinkByName("brz"); err != nil {
		t.Fatalf("LinkByName(brz) failed: %v", err)
	} else if p.Attrs().OperState == netlink.OperUp {
		t.Errorf("far end oper state = %v after this end went down, want not up", p.Attrs().OperState)
	}
}
