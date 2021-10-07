package gobgp

import (
	"testing"

	"google.golang.org/protobuf/proto"

	topopb "github.com/google/kne/proto/topo"
	"github.com/google/kne/topo/node"
)

func TestNode(t *testing.T) {
	tests := []struct {
		desc    string
		ni      *node.Impl
		wantPB  *topopb.Node
		wantErr string
	}{{
		desc: "valid pb",
		ni: &node.Impl{
			Proto: &topopb.Node{
				Name: "test_node",
			},
		},
		wantPB: &topopb.Node{
			Name: "test_node",
			Config: &topopb.Config{
				Image:        "hfam/gobgp:latest",
				Command:      []string{"/usr/local/bin/gobgpd", "-f", "/gobgp.conf", "-t", "yaml"},
				EntryCommand: "kubectl exec -it test_node -- /bin/bash",
				ConfigPath:   "/",
				ConfigFile:   "gobgp.conf",
			},
		},
	}}
	for _, tt := range tests {
		impl, _ := New(tt.ni)
		if !proto.Equal(tt.wantPB, impl.GetProto()) {
			t.Fatalf("invalid proto: got:\n%+v\nwant:\n%+v", impl.GetProto(), tt.wantPB)
		}
	}
}
