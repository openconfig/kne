package vmx

import (
	"context"
	"fmt"

	"google.golang.org/protobuf/proto"

	topopb "github.com/google/kne/proto/topo"
	"github.com/google/kne/topo/node"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

func New(pb *topopb.Node) (node.Implementation, error) {
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

func (n *Node) CreateNodeResource(_ context.Context, _ node.Interface) error {
	return status.Errorf(codes.Unimplemented, "Unimplemented")
}

func (n *Node) DeleteNodeResource(_ context.Context, _ node.Interface) error {
	return status.Errorf(codes.Unimplemented, "Unimplemented")
}

func defaults(pb *topopb.Node) *topopb.Node {
	return &topopb.Node{
		Config: &topopb.Config{
			Image:        "vmx:latest",
			Args:         []string{"--meshnet, --trace"},
			EntryCommand: fmt.Sprintf("kubectl exec -it %s -- sh", pb.Name),
			ConfigPath:   "/etc/quagga",
			ConfigFile:   "Quagga.conf",
			Sleep:        10,
		},
		Constraints: map[string]string{
			"memory": "5Gi",
			"cpu":    "1",
		},
	}
}

func init() {
	node.Register(topopb.Node_JUNIPER_VMX, New)
}
