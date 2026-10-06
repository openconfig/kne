// Package vxlan implements VXLAN overlay link creation and network interface management for meshnet daemon.
package vxlan

import (
	"fmt"
	"math/rand/v2"
	"net"
	"strconv"
	"strings"

	"github.com/containernetworking/plugins/pkg/ns"
	log "github.com/sirupsen/logrus"
	"github.com/vishvananda/netlink"

	mpb "github.com/openconfig/kne/third_party/meshnet/daemon/proto/meshnet/v1beta1"
	"github.com/openconfig/kne/third_party/meshnet/utils/wireutil"
)

var vxLanOvrlyLogger *log.Entry = nil

// VxLan represents the configuration for a VXLAN overlay interface.
type VxLan struct {
	ParentIF string
	IPAddr   net.IP
	ID       int
}

// InitLogger initializes the logrus logger for the VXLAN overlay daemon.
func InitLogger() {
	vxLanOvrlyLogger = log.WithFields(log.Fields{"daemon": "meshnetd", "overlay": "vxLAN"})
}

// CreateOrUpdate creates or updates the vxlan on the node.
func CreateOrUpdate(v *mpb.RemotePod) error {
	var srcIntf string
	var err error
	srcIntf = v.NodeIntf
	if srcIntf == "" {
		/// Looking up default interface
		_, srcIntf, err = getSource()
		if err != nil {
			return err
		}
	}

	var ipNet *net.IPNet
	if v.IntfIp != "" {
		ipAddr, ipSubnet, err := net.ParseCIDR(v.IntfIp)
		if err != nil {
			return fmt.Errorf(" MESHNETD: Error parsing CIDR %s: %s", v.IntfIp, err)
		}
		ipNet = &net.IPNet{
			IP:   ipAddr,
			Mask: ipSubnet.Mask,
		}
	}

	vxlan := VxLan{
		ParentIF: srcIntf,
		IPAddr:   net.ParseIP(v.PeerVtep),
		ID:       int(v.Vni),
	}
	vxLanOvrlyLogger.Infof("Created vxlan struct %+v", vxlan)

	// Try to read interface attributes from netlink
	link := getLinkFromNS(v.NetNs, v.IntfName)
	vxLanOvrlyLogger.Infof("Retrieved %s link from %s Netns: %+v", v.IntfName, v.NetNs, link)

	// Check if interface already exists
	vxlanLink, ok := link.(*netlink.Vxlan)
	vxLanOvrlyLogger.Infof("Is link %+v a VXLAN?: %s", vxlanLink, strconv.FormatBool(ok))
	if ok { // the link we've found is a vxlan link

		if !(vxlanLink.VxlanId == vxlan.ID && vxlanLink.Group.Equal(vxlan.IPAddr)) { // If Vxlan attrs are different

			// We remove the existing link and add a new one
			vxLanOvrlyLogger.Infof("Vxlan attrs are different: %d!=%d or %v!=%v", vxlanLink.VxlanId, vxlan.ID, vxlanLink.Group, vxlan.IPAddr)
			if err = removeLink(v.NetNs, v.IntfName); err != nil {
				return fmt.Errorf(" MESHNETD: Error when removing an old Vxlan interface: %s", err)
			}

			if err = makeVxLan(v.NetNs, v.IntfName, ipNet, vxlan); err != nil {
				if strings.Contains(err.Error(), "file exists") {
					vxLanOvrlyLogger.Infof(" MESHNETD: Error when creating a Vxlan interface, file exists")
				} else {
					return fmt.Errorf(" MESHNETD: Error when re-creating a Vxlan interface: %s", err)
				}
			}
		} // If Vxlan attrs are the same, do nothing

	} else { // the link we've found isn't a vxlan or doesn't exist

		vxLanOvrlyLogger.Infof("Link %+v we've found isn't a vxlan or doesn't exist", link)
		// If link exists but wasn't matched as vxlan, we need to delete it
		if link != nil {
			vxLanOvrlyLogger.Infof("Attempting to remove link %s from %s", v.IntfName, v.NetNs)
			if err = removeLink(v.NetNs, v.IntfName); err != nil {
				return fmt.Errorf(" MESHNETD: Error when removing an old non-Vxlan interface: %s", err)
			}
		}

		// Then we simply create a new one
		vxLanOvrlyLogger.Infof("Creating a VXLAN link: %v; inside the pod: %s in %s", vxlan, v.IntfName, v.NetNs)
		if err = makeVxLan(v.NetNs, v.IntfName, ipNet, vxlan); err != nil {
			vxLanOvrlyLogger.Errorf(" MESHNETD: Error when creating a new Vxlan interface: %s", err)
			return err
		}
	}

	// Tune txqueuelen inside the container netns (configurable via LINK_TXQUEUELEN)
	if podNs, err := ns.GetNS(v.NetNs); err == nil {
		_ = podNs.Do(func(_ ns.NetNS) error {
			if link, err := netlink.LinkByName(v.IntfName); err == nil {
				txqLen := wireutil.GetLinkTxQLen()
				if err := netlink.LinkSetTxQLen(link, txqLen); err != nil {
					vxLanOvrlyLogger.Warnf("failed to set txqueuelen %d on %s inside %s: %v", txqLen, v.IntfName, v.NetNs, err)
				}
			}
			return nil
		})
		podNs.Close()
	}

	return nil
}

// makeVxLan creates a VXLAN interface on the host and moves it into the container network namespace.
func makeVxLan(nsName, linkName string, ipNet *net.IPNet, vxlan VxLan) error {
	parentIF, err := netlink.LinkByName(vxlan.ParentIF)
	if err != nil {
		return fmt.Errorf("failed to get parent interface %s: %w", vxlan.ParentIF, err)
	}

	tempLinkName := fmt.Sprintf("vx-%08x", rand.Uint32())

	vxlanconf := &netlink.Vxlan{
		LinkAttrs: netlink.LinkAttrs{
			Name:   tempLinkName,
			TxQLen: 1000,
		},
		VxlanId:      vxlan.ID,
		VtepDevIndex: parentIF.Attrs().Index,
		Group:        vxlan.IPAddr,
		Port:         4789,
		Learning:     true,
		L2miss:       true,
		L3miss:       true,
	}

	if err := netlink.LinkAdd(vxlanconf); err != nil {
		return fmt.Errorf("failed to add vxlan %s: %w", tempLinkName, err)
	}

	link, err := netlink.LinkByName(tempLinkName)
	if err != nil {
		return fmt.Errorf("cannot get %s: %w", tempLinkName, err)
	}

	vethNs, err := ns.GetNS(nsName)
	if err != nil {
		_ = netlink.LinkDel(link)
		return fmt.Errorf("failed to get netns %s: %w", nsName, err)
	}
	defer vethNs.Close()

	if err := netlink.LinkSetNsFd(link, int(vethNs.Fd())); err != nil {
		_ = netlink.LinkDel(link)
		return fmt.Errorf("failed to move link %s to netns: %w", tempLinkName, err)
	}

	return vethNs.Do(func(_ ns.NetNS) error {
		podLink, err := netlink.LinkByName(tempLinkName)
		if err != nil {
			return fmt.Errorf("failed to lookup %q in netns: %w", tempLinkName, err)
		}

		if linkName != tempLinkName {
			if err := netlink.LinkSetName(podLink, linkName); err != nil {
				return fmt.Errorf("failed to rename link %s -> %s: %w", tempLinkName, linkName, err)
			}
		}

		if err := netlink.LinkSetUp(podLink); err != nil {
			return fmt.Errorf("failed to set %q up: %w", linkName, err)
		}

		if ipNet != nil {
			addr := &netlink.Addr{IPNet: ipNet}
			if err := netlink.AddrAdd(podLink, addr); err != nil {
				return fmt.Errorf("failed to add IP addr %v to %q: %w", addr, linkName, err)
			}
		}

		return nil
	})
}

// removeLink deletes an interface by name inside the specified network namespace.
func removeLink(nsName, linkName string) error {
	vethNs, err := ns.GetNS(nsName)
	if err != nil {
		return err
	}
	defer vethNs.Close()
	return vethNs.Do(func(_ ns.NetNS) error {
		link, err := netlink.LinkByName(linkName)
		if err != nil {
			return err
		}
		return netlink.LinkDel(link)
	})
}

// getLinkFromNS retrieves netlink.Link from NetNS
func getLinkFromNS(nsName string, linkName string) netlink.Link {
	// If namespace doesn't exist, do nothing and return empty result
	vethNs, err := ns.GetNS(nsName)
	if err != nil {
		return nil
	}
	defer vethNs.Close()
	// We can ignore the error returned here as we will create that interface instead
	var result netlink.Link
	err = vethNs.Do(func(_ ns.NetNS) error {
		var err error
		result, err = netlink.LinkByName(linkName)
		return err
	})
	if err != nil {
		vxLanOvrlyLogger.Warnf("failed to get link: %s", linkName)
	}

	return result
}

// Uses netlink to query the IP and LinkName of the interface with default route
func getSource() (string, string, error) {
	// Looking up a default route to get the intf and IP for vxlan
	r, err := netlink.RouteGet(net.IPv4(1, 1, 1, 1))
	if (err != nil) && len(r) < 1 {
		return "", "", fmt.Errorf(" MESHNETD: Error getting default route: %s\n%+v", err, r)
	}
	srcIP := r[0].Src.String()

	link, err := netlink.LinkByIndex(r[0].LinkIndex)
	if err != nil {
		return "", "", fmt.Errorf(" MESHNETD: Error looking up link by its index: %s", err)
	}
	srcIntf := link.Attrs().Name
	return srcIP, srcIntf, nil
}

func vxlanDifferent(l1 *netlink.Vxlan, l2 VxLan) bool {
	if l1.VxlanId != l2.ID {
		return false
	}
	if !l1.Group.Equal(l2.IPAddr) {
		return false
	}
	return true
}
