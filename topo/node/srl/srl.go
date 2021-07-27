package srl

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
			"cpu":    "0.5",
			"memory": "1Gi",
		},
		Services: map[uint32]*topopb.Service{
			443: {
				Name:    "ssl",
				Inside:  443,
				Outside: node.GetNextPort(),
			},
			22: {
				Name:    "ssh",
				Inside:  22,
				Outside: node.GetNextPort(),
			},
			57400: {
				Name:    "gnmi",
				Inside:  57400,
				Outside: node.GetNextPort(),
			},
		},
		Labels: map[string]string{
			"type": topopb.Node_NOKIA_SRL.String(),
		},
		Config: &topopb.Config{
			Image: "srlinux:latest",
			Command: []string{
				"/tini",
			},
			Args: []string{
				"--",
				"fixuid",
				"-q",
				"/entrypoint.sh",
				"sudo",
				"bash",
				"-c",
				"touch /.dockerenv && /opt/srlinux/bin/sr_linux",
			},
			Env: map[string]string{
				"SRLINUX": "1",
			},
			EntryCommand: fmt.Sprintf("kubectl exec -it %s -- sr_cli", pb.Name),
			ConfigPath:   "/etc/opt/srlinux",
			ConfigFile:   "config.json",
		},
	}
}

func init() {
	node.Register(topopb.Node_NOKIA_SRL, New)
}
