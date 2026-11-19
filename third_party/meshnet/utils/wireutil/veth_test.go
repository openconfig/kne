package wireutil_test

import (
	"fmt"
	"net"
	"os"
	"testing"

	"github.com/containernetworking/plugins/pkg/ns"
	"github.com/containernetworking/plugins/pkg/testutils"
	"github.com/openconfig/kne/third_party/meshnet/utils/wireutil"
	"github.com/vishvananda/netlink"
	"golang.org/x/sys/unix"
)

func TestConfigurePodLinks(t *testing.T) {
	if os.Getuid() != 0 {
		t.Skip("Test requires root privileges. Run test with sudo")
	}

	ns1, err := testutils.NewNS()
	if err != nil {
		t.Fatalf("Failed to create ns1: %v", err)
	}
	defer ns1.Close()

	ns2, err := testutils.NewNS()
	if err != nil {
		t.Fatalf("Failed to create ns2: %v", err)
	}
	defer ns2.Close()

	numLinks := 50
	links1 := make([]wireutil.PodLinkConfig, numLinks)
	links2 := make([]wireutil.PodLinkConfig, numLinks)

	for i := 0; i < numLinks; i++ {
		links1[i] = wireutil.PodLinkConfig{
			PodName:     "p1",
			PeerPodName: "p2",
			LinkUID:     int64(i + 1),
			KubeNs:      "testns",
			LocalIntf:   fmt.Sprintf("eth%d", i+1),
			LocalIP:     fmt.Sprintf("10.100.%d.1/30", i),
			MTU:         1500,
		}
		links2[i] = wireutil.PodLinkConfig{
			PodName:     "p2",
			PeerPodName: "p1",
			LinkUID:     int64(i + 1),
			KubeNs:      "testns",
			LocalIntf:   fmt.Sprintf("eth%d", i+1),
			LocalIP:     fmt.Sprintf("10.100.%d.2/30", i),
			MTU:         1500,
		}
	}

	// Step 1: Plumb all links for Pod 1 (p1).
	// This creates the host veth pairs, moves p1's ends into ns1, and leaves p2's ends waiting on the host.
	if err := wireutil.ConfigurePodLinks(ns1.Path(), links1); err != nil {
		t.Fatalf("ConfigurePodLinks(p1) failed: %v", err)
	}

	// Verify p2's waiting ends are present on the host
	for i := 0; i < numLinks; i++ {
		_, peerHostName := wireutil.HostVethNames("testns", "p1", "p2", int64(i+1))
		if _, err := netlink.LinkByName(peerHostName); err != nil {
			t.Fatalf("Expected waiting host veth end %s on host, got error: %v", peerHostName, err)
		}
	}

	// Step 2: Plumb all links for Pod 2 (p2).
	// This discovers the waiting host endpoints and moves them into ns2.
	if err := wireutil.ConfigurePodLinks(ns2.Path(), links2); err != nil {
		t.Fatalf("ConfigurePodLinks(p2) failed: %v", err)
	}

	// Verify all interfaces inside ns1
	err = ns1.Do(func(_ ns.NetNS) error {
		for i := 0; i < numLinks; i++ {
			name := fmt.Sprintf("eth%d", i+1)
			link, err := netlink.LinkByName(name)
			if err != nil {
				return fmt.Errorf("missing interface %s in ns1: %w", name, err)
			}
			if (link.Attrs().Flags & net.FlagUp) == 0 {
				return fmt.Errorf("interface %s in ns1 is not UP", name)
			}
			addrs, err := netlink.AddrList(link, netlink.FAMILY_V4)
			if err != nil || len(addrs) == 0 {
				return fmt.Errorf("missing IPv4 address on %s in ns1: %v", name, err)
			}
			expectedIP := fmt.Sprintf("10.100.%d.1/30", i)
			if addrs[0].IPNet.String() != expectedIP {
				return fmt.Errorf("expected IP %s on %s in ns1, got %s", expectedIP, name, addrs[0].IPNet.String())
			}
		}
		return nil
	})
	if err != nil {
		t.Fatalf("Verification failed in ns1: %v", err)
	}

	// Verify all interfaces inside ns2
	err = ns2.Do(func(_ ns.NetNS) error {
		for i := 0; i < numLinks; i++ {
			name := fmt.Sprintf("eth%d", i+1)
			link, err := netlink.LinkByName(name)
			if err != nil {
				return fmt.Errorf("missing interface %s in ns2: %w", name, err)
			}
			if (link.Attrs().Flags & net.FlagUp) == 0 {
				return fmt.Errorf("interface %s in ns2 is not UP", name)
			}
			addrs, err := netlink.AddrList(link, netlink.FAMILY_V4)
			if err != nil || len(addrs) == 0 {
				return fmt.Errorf("missing IPv4 address on %s in ns2: %v", name, err)
			}
			expectedIP := fmt.Sprintf("10.100.%d.2/30", i)
			if addrs[0].IPNet.String() != expectedIP {
				return fmt.Errorf("expected IP %s on %s in ns2, got %s", expectedIP, name, addrs[0].IPNet.String())
			}
		}
		return nil
	})
	if err != nil {
		t.Fatalf("Verification failed in ns2: %v", err)
	}

	// Step 3: Test crash/retry resiliency by running ConfigurePodLinks again (idempotence check).
	if err := wireutil.ConfigurePodLinks(ns1.Path(), links1); err != nil {
		t.Fatalf("Idempotent ConfigurePodLinks(p1) failed: %v", err)
	}
	if err := wireutil.ConfigurePodLinks(ns2.Path(), links2); err != nil {
		t.Fatalf("Idempotent ConfigurePodLinks(p2) failed: %v", err)
	}
}

// TestConfigurePodLinksPreservesAdminDown checks that re-running ConfigurePodLinks, as every
// reconcile does, leaves an interface that the workload shut down administratively down, so
// that its veth peer keeps seeing no carrier.
func TestConfigurePodLinksPreservesAdminDown(t *testing.T) {
	if os.Getuid() != 0 {
		t.Skip("Test requires root privileges. Run test with sudo")
	}

	ns1, err := testutils.NewNS()
	if err != nil {
		t.Fatalf("Failed to create ns1: %v", err)
	}
	defer func() { _ = ns1.Close() }()
	ns2, err := testutils.NewNS()
	if err != nil {
		t.Fatalf("Failed to create ns2: %v", err)
	}
	defer func() { _ = ns2.Close() }()

	links1 := []wireutil.PodLinkConfig{{
		PodName: "p1", PeerPodName: "p2", LinkUID: 1, KubeNs: "testns", LocalIntf: "eth1",
	}}
	links2 := []wireutil.PodLinkConfig{{
		PodName: "p2", PeerPodName: "p1", LinkUID: 1, KubeNs: "testns", LocalIntf: "eth1",
	}}
	reconcile := func() {
		t.Helper()
		if err := wireutil.ConfigurePodLinks(ns1.Path(), links1); err != nil {
			t.Fatalf("ConfigurePodLinks(p1) failed: %v", err)
		}
		if err := wireutil.ConfigurePodLinks(ns2.Path(), links2); err != nil {
			t.Fatalf("ConfigurePodLinks(p2) failed: %v", err)
		}
	}
	const upAndCarrier = unix.IFF_UP | unix.IFF_LOWER_UP

	reconcile()
	if got := linkFlags(t, ns1, "eth1") & upAndCarrier; got != upAndCarrier {
		t.Fatalf("after plumbing, p1 eth1 flags&(UP|LOWER_UP) = %#x, want %#x", got, upAndCarrier)
	}

	// The workload in p1 shuts the port down; reconciles must not bring it back up.
	setAdmin(t, ns1, "eth1", false)
	reconcile()
	reconcile()
	if got := linkFlags(t, ns1, "eth1") & unix.IFF_UP; got != 0 {
		t.Errorf("after reconcile, p1 eth1 is admin up, want it left admin down")
	}
	if got := linkFlags(t, ns2, "eth1") & upAndCarrier; got != unix.IFF_UP {
		t.Errorf("after reconcile, p2 eth1 flags&(UP|LOWER_UP) = %#x, want %#x (admin up, no carrier)", got, unix.IFF_UP)
	}

	// The workload brings the port back up; the peer regains carrier.
	setAdmin(t, ns1, "eth1", true)
	reconcile()
	if got := linkFlags(t, ns2, "eth1") & upAndCarrier; got != upAndCarrier {
		t.Errorf("after admin up, p2 eth1 flags&(UP|LOWER_UP) = %#x, want %#x", got, upAndCarrier)
	}
}
