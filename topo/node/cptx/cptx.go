package cptx

import (
	"context"
	"fmt"
	"io"

	topopb "github.com/google/kne/proto/topo"
	"github.com/google/kne/topo/node"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/proto"
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

func (n *Node) GenerateSelfSigned(ctx context.Context, ni node.Interface) error {
        return status.Errorf(codes.Unimplemented, "Unimplemented")
}

func (n *Node) ConfigPush(ctx context.Context, ns string, r io.Reader) error {
        return status.Errorf(codes.Unimplemented, "Unimplemented")
}

func (n *Node) CreateNodeResource(_ context.Context, _ node.Interface) error {
	return status.Errorf(codes.Unimplemented, "Unimplemented")
}

func (n *Node) DeleteNodeResource(_ context.Context, _ node.Interface) error {
	return status.Errorf(codes.Unimplemented, "Unimplemented")
}

func defaults(pb *topopb.Node) *topopb.Node {
	return &topopb.Node{
		Constraints: map[string]string{
			"cpu":    "8",
			"memory": "8Gi",
		},
		Services: map[uint32]*topopb.Service{
			443: &topopb.Service{
				Name:     "ssl",
				Inside:   443,
				NodePort: node.GetNextPort(),
			},
			22: &topopb.Service{
				Name:     "ssh",
				Inside:   22,
				NodePort: node.GetNextPort(),
			},
			50051: &topopb.Service{
				Name:     "gnmi",
				Inside:   50051,
				NodePort: node.GetNextPort(),
			},
		},
		Labels: map[string]string{
			"type": topopb.Node_JUNIPER_CEVO.String(),
		},
		Config: &topopb.Config{
			Image: "cptx:latest",
			Command: []string{
				"/entrypoint.sh",
			},
			Env: map[string]string{
			     "CPTX": "1",
			},
			EntryCommand: fmt.Sprintf("kubectl exec -it %s -- bash", pb.Name),
			ConfigPath:   "/home/evo/configdisk",
			ConfigFile:   "juniper.conf",
		},
	}
}

func init() {
	node.Register(topopb.Node_JUNIPER_CEVO, New)
}
