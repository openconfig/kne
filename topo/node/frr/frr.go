package frr

import (
	"fmt"
	"context"

	"google.golang.org/protobuf/proto"

	"k8s.io/client-go/kubernetes"
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

func (n *Node) CreateNodeResource(ctx context.Context, kClient kubernetes.Interface, ns string) error {
	return fmt.Errorf("Not Implemented")
}

func (n *Node) DeleteNodeResource(ctx context.Context, kClient kubernetes.Interface, ns string) error {
	return fmt.Errorf("Not Implemented")
}

func defaults(pb *topopb.Node) *topopb.Node {
	return &topopb.Node{
		Config: &topopb.Config{
			Image:      "frrouting/frr:latest",
			ConfigPath: "/etc/frr",
			ConfigFile: "frr.conf",
		},
	}
}

func init() {
	node.Register(topopb.Node_FRR, New)
}
