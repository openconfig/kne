package host

import (
	"fmt"

	topopb "github.com/google/kne/proto/topo"
	"github.com/google/kne/topo/node"
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
	cfg := &topopb.Config{
		Image:        "alpine:latest",
		Command:      []string{"/bin/sh", "-c", "sleep 2000000000000"},
		EntryCommand: fmt.Sprintf("kubectl exec -it %s -- sh", pb.Name),
		ConfigPath:   "/etc",
		ConfigFile:   "config",
	}
	pb.Config = cfg
	return nil
}

func init() {
	node.Register(topopb.Node_Host, New)
}
