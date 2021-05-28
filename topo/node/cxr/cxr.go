package cxr

import (
	"fmt"
	"context"

	"k8s.io/client-go/kubernetes"
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

func (n *Node) CreateNodeResource(ctx context.Context, kClient kubernetes.Interface, ns string) error {
	return fmt.Errorf("Not Implemented")
}

func (n *Node) DeleteNodeResource(ctx context.Context, kClient kubernetes.Interface, ns string) error {
	return fmt.Errorf("Not Implemented")
}

func defaults(pb *topopb.Node) *topopb.Node {
	return &topopb.Node{
		Constraints: map[string]string{
			"cpu":    "0.5",
			"memory": "1Gi",
		},
		Config: &topopb.Config{
			Image:        "cxr:latest",
			Args:         []string{"--meshnet", "--trace"},
			ConfigPath:   "/etc",
			ConfigFile:   "config",
			Sleep:        10,
			EntryCommand: fmt.Sprintf("kubectl exec -it %s -- sh", pb.Name),
		},
	}
}

func init() {
	node.Register(topopb.Node_CiscoCXR, New)
}
