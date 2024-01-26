package keysight

import (
	"testing"

	"github.com/openconfig/gnmi/errdiff"
	tpb "github.com/openconfig/kne/proto/topo"
	"github.com/openconfig/kne/topo/node"
	"google.golang.org/protobuf/encoding/prototext"
	"google.golang.org/protobuf/proto"
)

func TestNew(t *testing.T) {
	tests := []struct {
		desc    string
		nImpl   *node.Impl
		want    *tpb.Node
		wantErr string
	}{{
		desc:    "nil impl",
		wantErr: "nodeImpl cannot be nil",
	}, {
		desc:    "nil pb",
		wantErr: "nodeImpl.Proto cannot be nil",
		nImpl:   &node.Impl{},
	}, {
		desc: "empty pb defaults",
		nImpl: &node.Impl{
			Proto: &tpb.Node{
				Config: &tpb.Config{
					Image: "foo:bar",
				},
			},
		},
		want: &tpb.Node{
			Config: &tpb.Config{
				Image: "foo:bar",
			},
			Services: map[uint32]*tpb.Service{
				8443: {
					Name:   "https",
					Inside: 8443,
				},
				40051: {
					Name:   "grpc",
					Inside: 40051,
				},
				50051: {
					Name:   "gnmi",
					Inside: 50051,
				},
			},
			Labels: map[string]string{
				"ondatra-role": "ATE",
			},
		},
	}}
	for _, tt := range tests {
		t.Run(tt.desc, func(t *testing.T) {
			n, err := New(tt.nImpl)
			if s := errdiff.Substring(err, tt.wantErr); s != "" {
				t.Fatalf("unexpected error: got %v, want %s", err, s)
			}
			if tt.wantErr != "" {
				return
			}
			if !proto.Equal(n.GetProto(), tt.want) {
				t.Fatalf("New() failed: got\n%swant\n%s", prototext.Format(n.GetProto()), prototext.Format(tt.want))
			}
		})
	}
}
