// Package inclusterproxy implements a node type that acts as an in-cluster proxy.
package inclusterproxy

import (
	"context"
	"fmt"
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

func defaults(pb *tpb.Node) {
	if pb.Config == nil {
		pb.Config = &tpb.Config{}
	}
	if pb.Config.Image == "" {
		pb.Config.Image = defaultNode.Config.Image
	}
}

func (n *Node) validate() error {
	pb := n.GetProto()
	
	// Enforce exactly one interface: eth1
	if len(pb.Interfaces) != 1 {
		return fmt.Errorf("node %s: exactly one interface is required, found %d", n.Name(), len(pb.Interfaces))
	}
	if _, ok := pb.Interfaces["eth1"]; !ok {
		return fmt.Errorf("node %s: interface must be 'eth1'", n.Name())
	}

	// Ensure at least 1 service
	if len(pb.Services) == 0 {
		return fmt.Errorf("node %s: at least one service must be configured", n.Name())
	}

	// Warning if socat is not in command/args
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

	return nil
}

// Create overrides node.Impl.Create to enforce validation before creation if needed,
// but validation in New is preferred for early failure.
func (n *Node) Create(ctx context.Context) error {
	if err := n.validate(); err != nil {
		return err
	}
	return n.Impl.Create(ctx)
}

func init() {
	// tpb.Vendor_IN_CLUSTER_PROXY will be available once proto is compiled.
	// For now, we use the value 14 directly.
	node.Vendor(tpb.Vendor(14), New)
}
