package magna

import (
	"fmt"
	"testing"

	"github.com/google/go-cmp/cmp"
	"github.com/h-fam/errdiff"
	"github.com/openconfig/kne/topo/node"
	"google.golang.org/protobuf/testing/protocmp"

	tpb "github.com/openconfig/kne/proto/topo"
)

func TestNew(t *testing.T) {
	tests := []struct {
		desc    string
		inNode  *node.Impl
		want    *tpb.Node
		wantErr string
	}{{
		desc:    "nil impl",
		wantErr: "nodeImpl cannot be nil",
	}, {
		desc:    "nil pb",
		wantErr: "nodeImpl.Proto cannot be nil",
		inNode:  &node.Impl{},
	}, {
		desc: "empty pb",
		inNode: &node.Impl{
			Proto: &tpb.Node{},
		},
		want: &tpb.Node{
			Config: &tpb.Config{
				Command: []string{
					"/app/magna",
					"-v=2",
					"-alsologtostderr",
					"-port=40051",
					"-telemetry_port=50051",
					"-certfile=/data/cert.pem",
					"-keyfile=/data/key.pem",
				},
				EntryCommand: fmt.Sprintf("kubectl exec -it %s -- sh", ""),
				Image:        "magna:latest",
			},
			Services: map[uint32]*tpb.Service{
				40051: {
					Name:    "grpc",
					Inside:  40051,
					Outside: 40051,
				},
				50051: {
					Name:    "gnmi",
					Inside:  50051,
					Outside: 50051,
				},
			},
		},
	}, {
		desc: "provided service",
		inNode: &node.Impl{
			Proto: &tpb.Node{
				Config: &tpb.Config{
					Command: []string{"do", "run"},
				},
				Services: map[uint32]*tpb.Service{
					22: {
						Name:      "ssh",
						Inside:    22,
						Outside:   22,
						InsideIp:  "1.1.1.1",
						OutsideIp: "10.10.10.10",
					},
				},
			},
		},
		want: &tpb.Node{
			Config: &tpb.Config{
				Command:      []string{"do", "run"},
				EntryCommand: fmt.Sprintf("kubectl exec -it %s -- sh", ""),
				Image:        "magna:latest",
			},
			Services: map[uint32]*tpb.Service{
				22: {
					Name:      "ssh",
					Inside:    22,
					Outside:   22,
					InsideIp:  "1.1.1.1",
					OutsideIp: "10.10.10.10",
				},
				40051: {
					Name:    "grpc",
					Inside:  40051,
					Outside: 40051,
				},
				50051: {
					Name:    "gnmi",
					Inside:  50051,
					Outside: 50051,
				},
			},
		},
	}, {
		desc: "provided config command",
		inNode: &node.Impl{
			Proto: &tpb.Node{
				Config: &tpb.Config{
					Command: []string{"do", "run"},
				},
			},
		},
		want: &tpb.Node{
			Config: &tpb.Config{
				Command:      []string{"do", "run"},
				EntryCommand: fmt.Sprintf("kubectl exec -it %s -- sh", ""),
				Image:        "magna:latest",
			},
			Services: map[uint32]*tpb.Service{
				40051: {
					Name:    "grpc",
					Inside:  40051,
					Outside: 40051,
				},
				50051: {
					Name:    "gnmi",
					Inside:  50051,
					Outside: 50051,
				},
			},
		},
	}, {
		desc: "service already defined",
		inNode: &node.Impl{
			Proto: &tpb.Node{
				Config: &tpb.Config{
					Command: []string{"do", "run"},
				},
				Services: map[uint32]*tpb.Service{
					40051: {
						Name:    "foobar",
						Inside:  40051,
						Outside: 40051,
					},
				},
			},
		},
		want: &tpb.Node{
			Config: &tpb.Config{
				Command:      []string{"do", "run"},
				EntryCommand: fmt.Sprintf("kubectl exec -it %s -- sh", ""),
				Image:        "magna:latest",
			},
			Services: map[uint32]*tpb.Service{
				40051: {
					Name:    "foobar",
					Inside:  40051,
					Outside: 40051,
				},
				50051: {
					Name:    "gnmi",
					Inside:  50051,
					Outside: 50051,
				},
			},
		},
	}}
	for _, tt := range tests {
		t.Run(tt.desc, func(t *testing.T) {
			got, err := New(tt.inNode)
			if s := errdiff.Substring(err, tt.wantErr); s != "" {
				t.Fatalf("unexpected error: got %v, want %s", err, s)
			}
			if tt.wantErr != "" {
				return
			}
			if diff := cmp.Diff(got.GetProto(), tt.want, protocmp.Transform()); diff != "" {
				t.Fatalf("New() failed: diff(-got,+want):\n%s", diff)
			}
		})
	}
}
