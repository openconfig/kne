package gobgp

import (
	"testing"

	"google.golang.org/protobuf/proto"

	topopb "github.com/google/kne/proto/topo"
)

func TestNode(t *testing.T) {
	tests := []struct {
		desc    string
		pb      *topopb.Node
		wantPB  *topopb.Node
		wantErr string
	}{{
		desc:   "no pb",
		wantPB: defaults(nil),
	}, {
		desc: "valid pb",
		pb: &topopb.Node{
			Name: "test_node",
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
		impl, _ := New(tt.pb)
		n := impl.(*Node)
		if !proto.Equal(tt.wantPB, n.pb) {
			t.Fatalf("invalid proto: got:\n%+v\nwant:\n%+v", n.pb, tt.wantPB)
		}
	}
}
