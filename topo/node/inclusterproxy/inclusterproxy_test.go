package inclusterproxy

import (
	"testing"

	topopb "github.com/openconfig/kne/proto/topo"
	"github.com/openconfig/kne/topo/node"
	"google.golang.org/protobuf/proto"
)

func TestNew(t *testing.T) {
	tests := []struct {
		desc    string
		ni      *node.Impl
		wantPB  *topopb.Node
		wantErr string
	}{{
		desc: "valid pb with defaults",
		ni: &node.Impl{
			Proto: &topopb.Node{
				Name: "test_node",
				Interfaces: map[string]*topopb.Interface{
					"eth1": {},
				},
				Services: map[uint32]*topopb.Service{1: {}},
			},
		},
		wantPB: &topopb.Node{
			Name: "test_node",
			Config: &topopb.Config{
				Image: "nicolaka/netshoot:latest",
			},
			Interfaces: map[string]*topopb.Interface{
				"eth1": {},
			},
			Services: map[uint32]*topopb.Service{1: {}},
		},
	}}
	for _, tt := range tests {
		t.Run(tt.desc, func(t *testing.T) {
			impl, err := New(tt.ni)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if !proto.Equal(impl.GetProto(), tt.wantPB) {
				t.Fatalf("New() failed: got\n%v\nwant\n%v", impl.GetProto(), tt.wantPB)
			}
		})
	}
}
