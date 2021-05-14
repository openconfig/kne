package csr

import (
	"fmt"

	"google.golang.org/protobuf/proto"

	topopb "github.com/google/kne/proto/topo"
	"github.com/google/kne/topo/node"
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

func defaults(pb *topopb.Node) *topopb.Node {
	return &topopb.Node{
		Constraints: map[string]string{
			"cpu":    "0.5",
			"memory": "1Gi",
		},
		Config: &topopb.Config{
			Image:        "csr:latest",
			Args:         []string{"--meshnet"},
			ConfigPath:   "/etc",
			ConfigFile:   "config",
			Sleep:        10,
			EntryCommand: fmt.Sprintf("kubectl exec -it %s -- sh", pb.Name),
		},
	}
}

func init() {
	node.Register(topopb.Node_CiscoCSR, New)
}
