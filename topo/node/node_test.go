package node

import (
	"context"
	"testing"

	topopb "github.com/google/kne/proto/topo"
)

func NewNR(impl *Impl) (Node, error) {
	return &notResettable{Impl: impl}, nil
}

type notResettable struct {
	*Impl
}

type resettable struct {
	*notResettable
}

func (r *resettable) ResetCfg(ctx context.Context) error {
	return nil
}

func NewR(impl *Impl) (Node, error) {
	return &resettable{&notResettable{Impl: impl}}, nil
}

func TestReset(t *testing.T) {
	Register(topopb.Node_Type(1001), NewR)
	Register(topopb.Node_Type(1002), NewNR)
	n, err := New("test", &topopb.Node{Type: topopb.Node_Type(1001)}, nil, nil, "", "")
	if err != nil {
		t.Fatalf("failed to create node: %v", err)
	}
	r, ok := n.(Resetter)
	if !ok {
		t.Fatalf("Resettable node failed to type assert to resetter")
	}
	if err := r.ResetCfg(context.Background()); err != nil {
		t.Errorf("Resettable node failed to reset: %v", err)
	}
	nr, err := New("test", &topopb.Node{Type: topopb.Node_Type(1002)}, nil, nil, "", "")
	if err != nil {
		t.Fatalf("failed to create node: %v", err)
	}
	_, ok = nr.(Resetter)
	if ok {
		t.Errorf("Not-Resettable node type asserted to resetter")
	}
}
