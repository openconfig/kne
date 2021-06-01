package ceos

import (
	"fmt"

	"github.com/google/kne/proto/topo"
	topopb "github.com/google/kne/proto/topo"
	"github.com/google/kne/topo/node"
	"google.golang.org/protobuf/proto"
)

func New(pb *topopb.Node) (node.Interface, error) {
	cfg := defaults(pb)
	proto.Merge(cfg, pb)
	node.FixServices(cfg)
	return &Node{
		pb: cfg,
	}, nil
}

type Node struct {
	pb *topopb.Node
}

func (n *Node) Proto() *topopb.Node {
	return n.pb
}

func defaults(pb *topo.Node) *topopb.Node {
	return &topopb.Node{
		Constraints: map[string]string{
			"cpu":    "0.5",
			"memory": "1Gi",
		},
		Services: map[uint32]*topopb.Service{
			443: &topopb.Service{
				Name:    "ssl",
				Inside:  443,
				Outside: node.GetNextPort(),
			},
			22: &topopb.Service{
				Name:    "ssh",
				Inside:  22,
				Outside: node.GetNextPort(),
			},
			6030: &topopb.Service{
				Name:    "gnmi",
				Inside:  6030,
				Outside: node.GetNextPort(),
			},
		},
		Labels: map[string]string{
			"type": topopb.Node_AristaCEOS.String(),
		},
		Config: &topopb.Config{
			Image: "ceos:latest",
			Command: []string{
				"/sbin/init",
				"systemd.setenv=INTFTYPE=eth",
				"systemd.setenv=ETBA=1",
				"systemd.setenv=SKIP_ZEROTOUCH_BARRIER_IN_SYSDBINIT=1",
				"systemd.setenv=CEOS=1",
				"systemd.setenv=EOS_PLATFORM=ceoslab",
				"systemd.setenv=container=docker",
			},
			Env: map[string]string{
				"CEOS":                                "1",
				"EOS_PLATFORM":                        "ceoslab",
				"container":                           "docker",
				"ETBA":                                "1",
				"SKIP_ZEROTOUCH_BARRIER_IN_SYSDBINIT": "1",
				"INTFTYPE":                            "eth",
			},
			EntryCommand: fmt.Sprintf("kubectl exec -it %s -- Cli", pb.Name),
			ConfigPath:   "/mnt/flash",
			ConfigFile:   "startup-config",
		},
	}
}

func init() {
	node.Register(topopb.Node_AristaCEOS, New)
}
