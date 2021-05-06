package frr

import (
	"fmt"

	"google.golang.org/protobuf/proto"

	topopb "github.com/google/kne/proto/topo"
	"github.com/google/kne/topo/node"
)

func New(pb *topopb.Node) (node.Interface, error) {
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
		Image:        "frrouting/frr:latest",
		EntryCommand: fmt.Sprintf("kubectl exec -it %s -- sh", pb.Name),
		ConfigPath:   "/etc/frr",
		ConfigFile:   "frr.conf",
	}
	proto.Merge(cfg, pb.Config)
	pb.Config = cfg
	return nil
}

func init() {
	node.Register(topopb.Node_FRR, New)
}
