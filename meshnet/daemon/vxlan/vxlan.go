package vxlan

import (
	"fmt"
	"net"
	"strconv"
	"strings"

	"github.com/containernetworking/plugins/pkg/ns"
	"github.com/redhat-nfvpe/koko/api"
	log "github.com/sirupsen/logrus"
	"github.com/vishvananda/netlink"

	mpb "github.com/networkop/meshnet-cni/daemon/proto/meshnet/v1beta1"
)

var vxLanOvrlyLogger *log.Entry = nil

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

	// Creating koko Veth struct
	veth := api.VEth{
		NsName:   v.NetNs,
		LinkName: v.IntfName,
	}

	// Link IP is optional, only set it when it's provided
	if v.IntfIp != "" {
		ipAddr, ipSubnet, err := net.ParseCIDR(v.IntfIp)
		if err != nil {
			return fmt.Errorf(" MESHNETD: Error parsing CIDR %s: %s", v.IntfIp, err)
		}
		veth.IPAddr = []net.IPNet{{
			IP:   ipAddr,
			Mask: ipSubnet.Mask,
		}}
	}
	vxLanOvrlyLogger.Infof("Created koko Veth struct %+v", veth)

	// Creating koko vxlan struct
	vxlan := api.VxLan{
		ParentIF: srcIntf,
		IPAddr:   net.ParseIP(v.PeerVtep),
		ID:       int(v.Vni),
	}
	vxLanOvrlyLogger.Infof("Created koko vxlan struct %+v", vxlan)

	// Try to read interface attributes from netlink
	link := getLinkFromNS(veth.NsName, veth.LinkName)
	vxLanOvrlyLogger.Infof("Retrieved %s link from %s Netns: %+v", veth.LinkName, veth.NsName, link)

	// Check if interface already exists
	vxlanLink, ok := link.(*netlink.Vxlan)
	vxLanOvrlyLogger.Infof("Is link %+v a VXLAN?: %s", vxlanLink, strconv.FormatBool(ok))
	if ok { // the link we've found is a vxlan link

		if !(vxlanLink.VxlanId == vxlan.ID && vxlanLink.Group.Equal(vxlan.IPAddr)) { // If Vxlan attrs are different

			// We remove the existing link and add a new one
			vxLanOvrlyLogger.Infof("Vxlan attrs are different: %d!=%d or %v!=%v", vxlanLink.VxlanId, vxlan.ID, vxlanLink.Group, vxlan.IPAddr)
			if err = veth.RemoveVethLink(); err != nil {
				return fmt.Errorf(" MESHNETD: Error when removing an old Vxlan interface with koko: %s", err)
			}

			if err = api.MakeVxLan(veth, vxlan); err != nil {
				if strings.Contains(err.Error(), "file exists") {
					vxLanOvrlyLogger.Infof(" MESHNETD: Error when creating a Vxlan interface with koko, file exists")
				} else {
					return fmt.Errorf(" MESHNETD: Error when re-creating a Vxlan interface with koko: %s", err)
				}
			}
		} // If Vxlan attrs are the same, do nothing

	} else { // the link we've found isn't a vxlan or doesn't exist

		vxLanOvrlyLogger.Infof("Link %+v we've found isn't a vxlan or doesn't exist", link)
		// If link exists but wasn't matched as vxlan, we need to delete it
		if link != nil {
			vxLanOvrlyLogger.Infof("Attempting to remove link %+v", veth)
			if err = veth.RemoveVethLink(); err != nil {
				return fmt.Errorf(" MESHNETD: Error when removing an old non-Vxlan interface with koko: %s", err)
			}
		}

		// Then we simply create a new one
		vxLanOvrlyLogger.Infof("Creating a VXLAN link: %v; inside the pod: %v", vxlan, veth)
		if err = api.MakeVxLan(veth, vxlan); err != nil {
			if strings.Contains(err.Error(), "file exists") {
				vxLanOvrlyLogger.Warnf(" MESHNETD: Error when creating a Vxlan interface with koko, file exists")
			} else {
				vxLanOvrlyLogger.Errorf(" MESHNETD: Error when creating a new Vxlan interface with koko: %s", err)
				return err
			}
		}
	}

	return nil
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

func vxlanDifferent(l1 *netlink.Vxlan, l2 api.VxLan) bool {
	if l1.VxlanId != l2.ID {
		return false
	}
	if !l1.Group.Equal(l2.IPAddr) {
		return false
	}
	return true
}
