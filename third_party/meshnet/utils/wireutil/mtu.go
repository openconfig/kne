package wireutil

import (
	"fmt"

	"github.com/containernetworking/plugins/pkg/ns"
	"github.com/vishvananda/netlink"
)

// LinkMTU returns the MTU to give both ends of a link whose two ends request
// MTUs a and b, where zero means no request. Both ends of a link must have the
// same MTU, since a veth pair drops frames larger than the MTU of either end, so
// the larger request wins. When neither end makes a request, LinkMTU returns the
// LINK_MTU environment variable, or zero (leave the kernel default) if it is not
// set.
func LinkMTU(a, b int) int {
	if m := max(a, b); m > 0 {
		return m
	}
	return GetEnvInt("LINK_MTU", 0)
}

// SetLinkMTU sets the MTU of the interface ifName in the network namespace at
// podNsPath. It does nothing if mtu is not positive or the interface already
// has that MTU.
func SetLinkMTU(podNsPath, ifName string, mtu int) error {
	if mtu <= 0 {
		return nil
	}
	podNs, err := ns.GetNS(podNsPath)
	if err != nil {
		return fmt.Errorf("could not open netns %s: %w", podNsPath, err)
	}
	defer func() { _ = podNs.Close() }()

	return podNs.Do(func(_ ns.NetNS) error {
		link, err := netlink.LinkByName(ifName)
		if err != nil {
			return fmt.Errorf("failed to find %s in netns %s: %w", ifName, podNsPath, err)
		}
		if link.Attrs().MTU == mtu {
			return nil
		}
		if err := netlink.LinkSetMTU(link, mtu); err != nil {
			return fmt.Errorf("failed to set MTU %d on %s in netns %s: %w", mtu, ifName, podNsPath, err)
		}
		return nil
	})
}
