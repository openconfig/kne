// Package inclusterproxy implements a node type that acts as an in-cluster proxy.
package inclusterproxy

import (
	"context"
	"fmt"
	"net"
	"strings"

	tpb "github.com/openconfig/kne/proto/topo"
	"github.com/openconfig/kne/topo/node"
	log "k8s.io/klog/v2"
)

var (
	defaultNode = tpb.Node{
		Name: "default_incluster_proxy",
		Config: &tpb.Config{
			Image: "nicolaka/netshoot:latest",
		},
	}
)

func New(nodeImpl *node.Impl) (node.Node, error) {
	if nodeImpl == nil {
		return nil, fmt.Errorf("nodeImpl cannot be nil")
	}
	if nodeImpl.Proto == nil {
		return nil, fmt.Errorf("nodeImpl.Proto cannot be nil")
	}
	defaults(nodeImpl.Proto)
	n := &Node{
		Impl: nodeImpl,
	}
	if err := n.validate(); err != nil {
		return nil, err
	}
	return n, nil
}

type Node struct {
	*node.Impl
}

func calculateStaticIP(cidr string) (string, string, bool, error) {
	ip, ipNet, err := net.ParseCIDR(cidr)
	if err != nil {
		return "", "", false, err
	}
	ones, _ := ipNet.Mask.Size()
	
	ip4 := ip.To4()
	if ip4 != nil {
		if ones != 31 {
			return "", "", false, fmt.Errorf("only /31 mask is supported for IPv4 static IP calculation")
		}
		staticIP := make(net.IP, 4)
		copy(staticIP, ip4)
		staticIP[3] ^= 1
		return fmt.Sprintf("%s/31", staticIP.String()), ip.String(), false, nil
	}
	
	if ones != 127 {
		return "", "", false, fmt.Errorf("only /127 mask is supported for IPv6 static IP calculation")
	}
	staticIP := make(net.IP, 16)
	copy(staticIP, ip)
	staticIP[15] ^= 1
	return fmt.Sprintf("%s/127", staticIP.String()), ip.String(), true, nil
}

func defaults(pb *tpb.Node) {
	if pb.Config == nil {
		pb.Config = &tpb.Config{}
	}
	if pb.Config.Image == "" {
		pb.Config.Image = defaultNode.Config.Image
	}
	if pb.Labels == nil {
		return
	}
	peerIPLabel := pb.Labels["peer-ip"]
	targetPort := pb.Labels["target-port"]
	if peerIPLabel != "" && targetPort != "" && len(pb.Services) == 1 {
		staticIPWithMask, peerIPStr, isV6, err := calculateStaticIP(peerIPLabel)
		if err == nil {
			var insidePort uint32
			for k := range pb.Services {
				insidePort = pb.Services[k].Inside
				break
			}
			socatListen := "TCP-LISTEN"
			socatConnect := "TCP"
			if isV6 {
				socatListen = "TCP6-LISTEN"
				socatConnect = "TCP6"
			}
			pb.Config.Command = []string{"/bin/sh", "-c"}
			pb.Config.Args = []string{
				fmt.Sprintf("ip addr add %s dev eth1 && ip link set eth1 up && sleep 2 && /usr/bin/socat -d -d %s:%d,reuseaddr,fork %s:%s:%s", staticIPWithMask, socatListen, insidePort, socatConnect, peerIPStr, targetPort),
			}
		}
	}
}

func (n *Node) validate() error {
	pb := n.GetProto()
	
	// Enforce proxy-pool-for label
	if pb.Labels == nil || pb.Labels["proxy-pool-for"] == "" {
		return fmt.Errorf("node %s: label 'proxy-pool-for' is required", n.Name())
	}

	// Enforce exactly one interface: eth1
	if len(pb.Interfaces) != 1 {
		return fmt.Errorf("node %s: exactly one interface is required, found %d", n.Name(), len(pb.Interfaces))
	}
	eth1, ok := pb.Interfaces["eth1"]
	if !ok {
		return fmt.Errorf("node %s: interface must be 'eth1'", n.Name())
	}

	// Enforce link to target node
	peerNodeName := pb.Labels["proxy-pool-for"]
	if eth1.PeerName != peerNodeName {
		return fmt.Errorf("node %s: eth1 must be connected to %s, found %s", n.Name(), peerNodeName, eth1.PeerName)
	}

	// Ensure exactly 1 service
	if len(pb.Services) != 1 {
		return fmt.Errorf("node %s: exactly one service must be configured, found %d", n.Name(), len(pb.Services))
	}

	peerIPLabel := pb.Labels["peer-ip"]
	targetPort := pb.Labels["target-port"]
	if peerIPLabel != "" || targetPort != "" {
		if peerIPLabel == "" || targetPort == "" {
			return fmt.Errorf("node %s: both 'peer-ip' and 'target-port' labels must be provided together for automatic socat generation", n.Name())
		}
		if _, _, _, err := calculateStaticIP(peerIPLabel); err != nil {
			return fmt.Errorf("node %s: invalid 'peer-ip' label: %v", n.Name(), err)
		}
	} else {
		// Warning if socat is not in command/args and generation is skipped
		hasSocat := false
		for _, c := range pb.Config.Command {
			if strings.Contains(c, "socat") {
				hasSocat = true
				break
			}
		}
		if !hasSocat {
			for _, a := range pb.Config.Args {
				if strings.Contains(a, "socat") {
					hasSocat = true
					break
				}
			}
		}
		if !hasSocat {
			log.Warningf("node %s: command/args do not contain 'socat', which may be needed for proxying", n.Name())
		}
	}

	return nil
}

func (n *Node) Create(ctx context.Context) error {
	if err := n.validate(); err != nil {
		return err
	}
	return n.Impl.Create(ctx)
}

func init() {
	node.Vendor(tpb.Vendor(14), New)
}
