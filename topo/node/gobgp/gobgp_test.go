package gobgp

import (
	"testing"

	"google.golang.org/protobuf/encoding/prototext"
	"google.golang.org/protobuf/proto"

	topopb "github.com/google/kne/proto/topo"
	"github.com/google/kne/topo/node"
	"github.com/h-fam/errdiff"
)

func TestNew(t *testing.T) {
	tests := []struct {
		desc    string
		ni      *node.Impl
		wantPB  *topopb.Node
		wantErr string
	}{{
		desc:    "nil node impl",
		wantErr: "nodeImpl cannot be nil",
	}, {
		desc:    "nil pb",
		wantErr: "nodeImpl.Proto cannot be nil",
		ni:      &node.Impl{},
	}, {
		desc: "test defaults",
		ni: &node.Impl{
			Proto: &topopb.Node{
				Name: "test_node",
				Config: &topopb.Config{
					Image:   "foobar",
					Command: []string{"run", "some", "command"},
				},
			},
		},
		wantPB: &topopb.Node{
			Name: "test_node",
			Config: &topopb.Config{
				Image:        "foobar",
				Command:      []string{"run", "some", "command"},
				EntryCommand: "kubectl exec -it test_node -- /bin/bash",
				ConfigPath:   "/",
				ConfigFile:   "gobgp.conf",
			},
		},
	}, {
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
		t.Run(tt.desc, func(t *testing.T) {
			impl, err := New(tt.ni)
			if s := errdiff.Substring(err, tt.wantErr); s != "" {
				t.Fatalf("unexpected error: got: %v, want: %s", err, s)
			}
			if tt.wantErr != "" {
				return
			}
			if !proto.Equal(impl.GetProto(), tt.wantPB) {
				t.Fatalf("New() failed: got\n%swant\n%s", prototext.Format(impl.GetProto()), prototext.Format(tt.wantPB))
			}
		})
	}
}
