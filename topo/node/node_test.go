package node

import (
	"context"
	"testing"

	topopb "github.com/google/kne/proto/topo"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

func NewNR(pb *topopb.Node) (Implementation, error) {
	return &notResetable{pb: pb}, nil
}

type notResetable struct {
	pb *topopb.Node
}

func (nr *notResetable) Proto() *topopb.Node {
	return nr.pb
}

func (nr *notResetable) CreateNodeResource(_ context.Context, _ Interface) error {
	return status.Errorf(codes.Unimplemented, "Unimplemented")
}

func (nr *notResetable) GetNodeResourceStatus(_ context.Context, _ Interface) (NodeStatus, error) {
	return NodeStatus{}, status.Errorf(codes.Unimplemented, "Unimplemented")
}

func (nr *notResetable) DeleteNodeResource(_ context.Context, _ Interface) error {
	return status.Errorf(codes.Unimplemented, "Unimplemented")
}

type resetable struct {
	*notResetable
}

func (r *resetable) ResetCfg(ctx context.Context, ni Interface) error {
	return nil
}

func NewR(pb *topopb.Node) (Implementation, error) {
	return &resetable{&notResetable{pb: pb}}, nil
}

func TestReset(t *testing.T) {
	Register(topopb.Node_Type(1001), NewR)
	Register(topopb.Node_Type(1002), NewNR)
	n, err := New("test", &topopb.Node{Type: topopb.Node_Type(1001)}, nil, nil)
	if err != nil {
		t.Fatalf("failed to create node: %v", err)
	}
	if err := n.ResetCfg(context.Background()); err != nil {
		t.Errorf("Resetable node failed to reset: %v", err)
	}
	nr, err := New("test", &topopb.Node{Type: topopb.Node_Type(1002)}, nil, nil)
	if err != nil {
		t.Fatalf("failed to create node: %v", err)
	}
	if err := nr.ResetCfg(context.Background()); err == nil {
		t.Errorf("Non-resetable node was able to reset")
	}
}
