package wireutil

import (
	"fmt"
	"hash/fnv"
	"os"
	"sort"
	"strings"

	"github.com/containernetworking/plugins/pkg/ns"
	"github.com/safchain/ethtool"
	log "github.com/sirupsen/logrus"
	"github.com/vishvananda/netlink"
)

// PodLinkConfig describes a single interface to be configured inside a pod network namespace.
type PodLinkConfig struct {
	PodName     string // Local pod name (e.g. "p1")
	PeerPodName string // Peer pod name (e.g. "p2")
	LinkUID     int64  // Unique link ID in topology (e.g. 14)
	KubeNs      string // Kubernetes namespace (e.g. "1500links")
	LocalIntf   string // Target interface name inside local pod (e.g. "eth14")
	LocalIP     string // Local CIDR (e.g. "10.10.0.1/30")
	MTU         int    // Interface MTU (default 1500 if <= 0)
}

var txOffloadDisabledMap = map[string]bool{
	"tx-checksum-ipv4":             false,
	"tx-checksum-ipv6":             false,
	"tx-checksum-ip-generic":       false,
	"tx-tcp-segmentation":          false,
	"tx-tcp6-segmentation":         false,
	"tx-checksum-fcoe-crc":         false,
	"tx-checksum-sctp":             false,
	"tx-tcp-ecn-segmentation":      false,
	"tx-tcp-mangleid-segmentation": false,
}

func disableTxOffload(etlHndl *ethtool.Ethtool, intfName string) error {
	if etlHndl == nil {
		return nil
	}
	return etlHndl.Change(intfName, txOffloadDisabledMap)
}

// HostVethNames computes deterministic 14-character host interface names for both ends
// of a veth pair connecting (podName, peerPodName) for linkUID in kubeNs.
// Returns (localHostName, peerHostName).
func HostVethNames(kubeNs, podName, peerPodName string, linkUID int64) (string, string) {
	h := fnv.New32a()
	pods := []string{podName, peerPodName}
	sort.Strings(pods)
	fmt.Fprintf(h, "%s/%s/%s/%d", kubeNs, pods[0], pods[1], linkUID)
	sum := h.Sum32()

	side0 := fmt.Sprintf("vnm-%08x-0", sum)
	side1 := fmt.Sprintf("vnm-%08x-1", sum)

	if podName == pods[0] {
		return side0, side1
	}
	return side1, side0
}

// ConfigurePodLinks batch-creates and configures all links terminating inside a single pod
// network namespace (podNsPath).
//
// Uses deterministic host veth naming (HostVethNames) so that:
// - If the veth pair has not been created yet, it creates the host veth pair, moves the local end
//   into podNsPath, and leaves the peer end waiting on the host for the peer pod to claim.
// - If the peer pod already created the host veth pair, it finds the waiting local end on the host
//   and moves it into podNsPath.
// - If interrupted or restarted halfway through, it discovers already-moved interfaces and resumes
//   idempotently.
func ConfigurePodLinks(podNsPath string, links []PodLinkConfig) error {
	if len(links) == 0 {
		return nil
	}

	podNs, err := ns.GetNS(podNsPath)
	if err != nil {
		return fmt.Errorf("could not open netns %s: %w", podNsPath, err)
	}
	defer podNs.Close()

	// 1. Discover all existing interface names currently inside podNs in one single pass.
	existingInContainer := make(map[string]bool)
	err = podNs.Do(func(_ ns.NetNS) error {
		list, err := netlink.LinkList()
		if err != nil {
			return err
		}
		for _, l := range list {
			existingInContainer[l.Attrs().Name] = true
		}
		return nil
	})
	if err != nil {
		return fmt.Errorf("failed to list links in netns %s: %w", podNsPath, err)
	}

	// 2. Host pass: For any link not yet moved into podNs, either find its waiting end on the host
	//    or create the deterministic host veth pair and move our end in. Zero netns switches.
	for _, cfg := range links {
		localHostName, peerHostName := HostVethNames(cfg.KubeNs, cfg.PodName, cfg.PeerPodName, cfg.LinkUID)

		// If either the target name (LocalIntf) or temporary name (localHostName) is already in podNs,
		// the interface has already been moved inside.
		if existingInContainer[cfg.LocalIntf] || existingInContainer[localHostName] {
			continue
		}

		// Check if localHostName is already waiting on the host (created by peer pod or previous run).
		localLink, err := netlink.LinkByName(localHostName)
		if err != nil {
			// Not on host -> create the host veth pair.
			mtu := cfg.MTU
			if mtu <= 0 {
				mtu = 1500
			}
			veth := &netlink.Veth{
				LinkAttrs: netlink.LinkAttrs{
					Name: localHostName,
					MTU:  mtu,
				},
				PeerName: peerHostName,
			}
			if err := netlink.LinkAdd(veth); err != nil && !os.IsExist(err) && !strings.Contains(err.Error(), "file exists") {
				return fmt.Errorf("failed to create host veth pair (%s, %s): %w", localHostName, peerHostName, err)
			}
			localLink, err = netlink.LinkByName(localHostName)
			if err != nil {
				return fmt.Errorf("failed to find host veth end %s after LinkAdd: %w", localHostName, err)
			}
		}

		if err := netlink.LinkSetNsFd(localLink, int(podNs.Fd())); err != nil {
			return fmt.Errorf("failed to move %s into netns %s: %w", localHostName, podNsPath, err)
		}
		existingInContainer[localHostName] = true
	}

	// 3. Perform 1 single netns switch into podNs to configure all interfaces terminating here.
	err = podNs.Do(func(_ ns.NetNS) error {
		etlHndl, _ := ethtool.NewEthtool()
		if etlHndl != nil {
			defer etlHndl.Close()
		}

		for _, cfg := range links {
			localHostName, _ := HostVethNames(cfg.KubeNs, cfg.PodName, cfg.PeerPodName, cfg.LinkUID)

			var link netlink.Link
			var err error
			link, err = netlink.LinkByName(localHostName)
			if err != nil {
				link, err = netlink.LinkByName(cfg.LocalIntf)
				if err != nil {
					return fmt.Errorf("interface %s (or temp %s) not found inside netns %s: %w", cfg.LocalIntf, localHostName, podNsPath, err)
				}
			}

			if link.Attrs().Name != cfg.LocalIntf {
				if err := netlink.LinkSetName(link, cfg.LocalIntf); err != nil {
					if strings.Contains(err.Error(), "file exists") || os.IsExist(err) {
						log.Warnf("ConfigurePodLinks: %s already exists in %s, removing duplicate/stale %s", cfg.LocalIntf, podNsPath, link.Attrs().Name)
						netlink.LinkDel(link)
						continue
					}
					return fmt.Errorf("failed to rename %s -> %s inside %s: %w", link.Attrs().Name, cfg.LocalIntf, podNsPath, err)
				}
			}

			if err := netlink.LinkSetUp(link); err != nil {
				return fmt.Errorf("failed to set %s UP inside %s: %w", cfg.LocalIntf, podNsPath, err)
			}

			if cfg.LocalIP != "" {
				addr, err := netlink.ParseAddr(cfg.LocalIP)
				if err != nil {
					return fmt.Errorf("invalid CIDR %q for %s: %w", cfg.LocalIP, cfg.LocalIntf, err)
				}
				if err := netlink.AddrAdd(link, addr); err != nil {
					if !strings.Contains(err.Error(), "file exists") && !os.IsExist(err) {
						return fmt.Errorf("failed to add IP %s to %s inside %s: %w", cfg.LocalIP, cfg.LocalIntf, podNsPath, err)
					}
				}
			}

			if err := disableTxOffload(etlHndl, cfg.LocalIntf); err != nil {
				log.Warnf("ConfigurePodLinks: failed to set tx-checksum-off on %s inside %s: %v", cfg.LocalIntf, podNsPath, err)
			}
		}
		return nil
	})

	return err
}
