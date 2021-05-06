package ceos

import (
	"fmt"

	topopb "github.com/google/kne/proto/topo"
	"github.com/google/kne/topo/node"
	"google.golang.org/protobuf/proto"
)

func New(pb *topopb.Node) (node.Interface, error) {
	if err := defaults(pb); err != nil {
		return nil, err
	}
	return &Node{
		pb: pb,
	}, nil
}

type Node struct {
	pb *topopb.Node
}

func (n *Node) Proto() *topopb.Node {
	return n.pb
}

func defaults(pb *topopb.Node) error {
	cfg := &topopb.Node{
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
	for _, v := range pb.Services {
		if v.Outside == 0 {
			v.Outside = node.GetNextPort()
		}
	}
	proto.Merge(pb, cfg)
	return nil
}

func init() {
	node.Register(topopb.Node_AristaCEOS, New)
}
