package wireutil_test

import (
	"errors"
	"os"
	"testing"
	"time"

	"github.com/containernetworking/plugins/pkg/ns"
	"github.com/containernetworking/plugins/pkg/testutils"
	"github.com/openconfig/kne/third_party/meshnet/utils/wireutil"
	"github.com/vishvananda/netlink"
	"golang.org/x/sys/unix"
)

// linkFlags returns the flags of ifName inside netNS.
func linkFlags(t *testing.T, netNS ns.NetNS, ifName string) uint32 {
	t.Helper()
	var flags uint32
	if err := netNS.Do(func(ns.NetNS) error {
		l, err := netlink.LinkByName(ifName)
		if err != nil {
			return err
		}
		flags = l.Attrs().RawFlags
		return nil
	}); err != nil {
		t.Fatalf("LinkByName(%q) failed: %v", ifName, err)
	}
	return flags
}

func setAdmin(t *testing.T, netNS ns.NetNS, ifName string, up bool) {
	t.Helper()
	if err := netNS.Do(func(ns.NetNS) error {
		l, err := netlink.LinkByName(ifName)
		if err != nil {
			return err
		}
		if up {
			return netlink.LinkSetUp(l)
		}
		return netlink.LinkSetDown(l)
	}); err != nil {
		t.Fatalf("setting %q admin up=%t failed: %v", ifName, up, err)
	}
}

func TestTAPCarrierAndAdminWatch(t *testing.T) {
	if os.Getuid() != 0 {
		t.Skip("Test requires root privileges. Run test with sudo")
	}
	netNS, err := testutils.NewNS()
	if err != nil {
		t.Fatalf("Failed to create netns: %v", err)
	}
	defer func() { _ = netNS.Close() }()

	const ifName = "eth1"
	tap, err := wireutil.CreateOrAttachTAP(netNS.Path(), ifName, "")
	if err != nil {
		t.Fatalf("CreateOrAttachTAP() failed: %v", err)
	}
	defer func() { _ = tap.Close() }()

	type change struct {
		name    string
		adminUp bool
	}
	changes := make(chan change, 16)
	stop, err := wireutil.WatchAdminState(netNS.Path(), func(name string, adminUp bool) {
		if name == ifName {
			changes <- change{name, adminUp}
		}
	})
	if err != nil {
		t.Fatalf("WatchAdminState() failed: %v", err)
	}
	defer stop()

	expect := func(wantUp bool) {
		t.Helper()
		select {
		case c := <-changes:
			if c.adminUp != wantUp {
				t.Fatalf("admin change for %s: got up=%t, want up=%t", ifName, c.adminUp, wantUp)
			}
		case <-time.After(5 * time.Second):
			t.Fatalf("timed out waiting for admin change up=%t", wantUp)
		}
	}
	expect(true) // existing link

	// Carrier changes must not be reported as admin changes.
	if err := wireutil.SetTAPCarrier(tap, false); err != nil {
		t.Fatalf("SetTAPCarrier(false) failed: %v", err)
	}
	if got := linkFlags(t, netNS, ifName); got&unix.IFF_LOWER_UP != 0 {
		t.Errorf("after SetTAPCarrier(false): flags %#x have IFF_LOWER_UP", got)
	}
	if err := wireutil.SetTAPCarrier(tap, true); err != nil {
		t.Fatalf("SetTAPCarrier(true) failed: %v", err)
	}
	if got := linkFlags(t, netNS, ifName); got&unix.IFF_LOWER_UP == 0 {
		t.Errorf("after SetTAPCarrier(true): flags %#x lack IFF_LOWER_UP", got)
	}

	setAdmin(t, netNS, ifName, false)
	expect(false)
	setAdmin(t, netNS, ifName, true)
	expect(true)

	select {
	case c := <-changes:
		t.Errorf("unexpected admin change: %+v", c)
	case <-time.After(200 * time.Millisecond):
	}

	stop()
	stop() // stop is idempotent
}

// TestCreateOrAttachTAPNonBlocking checks that the TAP is opened O_NONBLOCK, so
// that Go's poller parks readers instead of pinning an OS thread per wire, and
// that Close unblocks a pending Read.
func TestCreateOrAttachTAPNonBlocking(t *testing.T) {
	if os.Getuid() != 0 {
		t.Skip("Test requires root privileges. Run test with sudo")
	}
	netNS, err := testutils.NewNS()
	if err != nil {
		t.Fatalf("Failed to create netns: %v", err)
	}
	defer func() { _ = netNS.Close() }()

	tap, err := wireutil.CreateOrAttachTAP(netNS.Path(), "eth1", "")
	if err != nil {
		t.Fatalf("CreateOrAttachTAP() failed: %v", err)
	}

	// Use SyscallConn rather than Fd(): Fd() would put the fd back in blocking mode.
	rc, err := tap.SyscallConn()
	if err != nil {
		t.Fatalf("SyscallConn() failed: %v", err)
	}
	var flags int
	var flagsErr error
	if err := rc.Control(func(fd uintptr) {
		flags, flagsErr = unix.FcntlInt(fd, unix.F_GETFL, 0)
	}); err != nil {
		t.Fatalf("Control() failed: %v", err)
	}
	if flagsErr != nil {
		t.Fatalf("fcntl(F_GETFL) failed: %v", flagsErr)
	}
	if flags&unix.O_NONBLOCK == 0 {
		t.Errorf("TAP fd flags %#x lack O_NONBLOCK", flags)
	}

	// The kernel may emit frames (e.g. IPv6 neighbor discovery) once the link
	// is up, so keep reading until Read fails.
	readErr := make(chan error, 1)
	go func() {
		buf := make([]byte, 65535)
		for {
			if _, err := tap.Read(buf); err != nil {
				readErr <- err
				return
			}
		}
	}()
	select {
	case err := <-readErr:
		t.Fatalf("Read() on open TAP failed: %v", err)
	case <-time.After(100 * time.Millisecond):
	}
	if err := tap.Close(); err != nil {
		t.Fatalf("Close() failed: %v", err)
	}
	select {
	case err := <-readErr:
		if !errors.Is(err, os.ErrClosed) {
			t.Errorf("Read() after Close() = %v, want %v", err, os.ErrClosed)
		}
	case <-time.After(time.Second):
		t.Fatal("Close() did not unblock a pending Read()")
	}
}

func TestCreateOrAttachTAPPreservesAdminDown(t *testing.T) {
	if os.Getuid() != 0 {
		t.Skip("Test requires root privileges. Run test with sudo")
	}
	netNS, err := testutils.NewNS()
	if err != nil {
		t.Fatalf("Failed to create netns: %v", err)
	}
	defer func() { _ = netNS.Close() }()

	const ifName = "eth1"
	tap, created, err := wireutil.OpenOrCreateTAPWithMTU(netNS.Path(), ifName, "", 0)
	if err != nil {
		t.Fatalf("OpenOrCreateTAPWithMTU() failed: %v", err)
	}
	if !created {
		t.Fatalf("initial OpenOrCreateTAPWithMTU() created = false, want true")
	}
	if got := linkFlags(t, netNS, ifName); got&unix.IFF_UP == 0 {
		t.Fatalf("after initial OpenOrCreateTAPWithMTU(): flags %#x lack IFF_UP", got)
	}

	// Simulate NOS shutting the port down and meshnetd exiting while TUNSETPERSIST keeps the TAP.
	setAdmin(t, netNS, ifName, false)
	if err := tap.Close(); err != nil {
		t.Fatalf("tap.Close() failed: %v", err)
	}

	// Re-attaching via CreateOrAttachTAP must preserve admin-down state.
	reattached, err := wireutil.CreateOrAttachTAP(netNS.Path(), ifName, "")
	if err != nil {
		t.Fatalf("re-attach CreateOrAttachTAP() failed: %v", err)
	}
	defer func() { _ = reattached.Close() }()
	if got := linkFlags(t, netNS, ifName); got&unix.IFF_UP != 0 {
		t.Errorf("after re-attach: flags %#x unexpectedly have IFF_UP set", got)
	}

	// Simulate NOS bringing the port back up, then another daemon restart via OpenOrCreateTAPWithMTU.
	setAdmin(t, netNS, ifName, true)
	if err := reattached.Close(); err != nil {
		t.Fatalf("reattached.Close() failed: %v", err)
	}
	reattached2, created, err := wireutil.OpenOrCreateTAPWithMTU(netNS.Path(), ifName, "", 9000)
	if err != nil {
		t.Fatalf("re-attach OpenOrCreateTAPWithMTU() failed: %v", err)
	}
	defer func() { _ = reattached2.Close() }()
	if created {
		t.Fatalf("re-attach OpenOrCreateTAPWithMTU() created = true, want false")
	}
	if got := linkFlags(t, netNS, ifName); got&unix.IFF_UP == 0 {
		t.Errorf("after re-attach of admin-up TAP: flags %#x lack IFF_UP", got)
	}
	if err := netNS.Do(func(ns.NetNS) error {
		l, err := netlink.LinkByName(ifName)
		if err != nil {
			return err
		}
		if l.Attrs().MTU == 9000 {
			t.Errorf("re-attach overwrote MTU to %d", l.Attrs().MTU)
		}
		return nil
	}); err != nil {
		t.Fatalf("inspecting link failed: %v", err)
	}
}

func TestOpenOrCreateTAPFailureLeavesNoDevice(t *testing.T) {
	if os.Getuid() != 0 {
		t.Skip("Test requires root privileges. Run test with sudo")
	}
	netNS, err := testutils.NewNS()
	if err != nil {
		t.Fatalf("Failed to create netns: %v", err)
	}
	defer func() { _ = netNS.Close() }()

	const ifName = "eth1"
	if _, _, err := wireutil.OpenOrCreateTAPWithMTU(netNS.Path(), ifName, "not-a-cidr", 0); err == nil {
		t.Fatal("OpenOrCreateTAPWithMTU(invalid CIDR) succeeded, want error")
	}
	if _, _, err := wireutil.OpenOrCreateTAPWithMTU(netNS.Path(), ifName, "", 1<<30); err == nil {
		t.Fatal("OpenOrCreateTAPWithMTU(invalid MTU) succeeded, want error")
	}

	if err := netNS.Do(func(ns.NetNS) error {
		_, err := netlink.LinkByName(ifName)
		var notFound netlink.LinkNotFoundError
		if !errors.As(err, &notFound) {
			return errors.New("expected TAP device not to persist after failed creation")
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}

	tap, created, err := wireutil.OpenOrCreateTAPWithMTU(netNS.Path(), ifName, "", 0)
	if err != nil {
		t.Fatalf("retry OpenOrCreateTAPWithMTU() failed: %v", err)
	}
	defer func() { _ = tap.Close() }()
	if !created {
		t.Fatal("retry OpenOrCreateTAPWithMTU() created = false, want true")
	}
	if got := linkFlags(t, netNS, ifName); got&unix.IFF_UP == 0 {
		t.Errorf("after retry: flags %#x lack IFF_UP", got)
	}
}
