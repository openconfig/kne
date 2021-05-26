package host

import (
	"fmt"
	"context"

	"k8s.io/client-go/kubernetes"
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

func (n *Node) CreateNodeResource(ctx context.Context, kClient kubernetes.Interface, ns string) error {
	return fmt.Errorf("Not Implemented")
}

func (n *Node) DeleteNodeResource(ctx context.Context, kClient kubernetes.Interface, ns string) error {
	return fmt.Errorf("Not Implemented")
}

func defaults(pb *topopb.Node) *topopb.Node {
	return &topopb.Node{
		Config: &topopb.Config{
			Image:        "alpine:latest",
			Command:      []string{"/bin/sh", "-c", "sleep 2000000000000"},
			EntryCommand: fmt.Sprintf("kubectl exec -it %s -- sh", pb.Name),
			ConfigPath:   "/etc",
			ConfigFile:   "config",
		},
	}
}

func init() {
	node.Register(topopb.Node_Host, New)
}
