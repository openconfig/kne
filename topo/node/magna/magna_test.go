package magna

import (
	"fmt"
	"testing"

	"github.com/google/go-cmp/cmp"
	"github.com/h-fam/errdiff"
	"github.com/openconfig/kne/topo/node"
	"google.golang.org/protobuf/testing/protocmp"

	topopb "github.com/openconfig/kne/proto/topo"
)

func TestNew(t *testing.T) {
	tests := []struct {
		desc    string
		nImpl   *node.Impl
		want    *topopb.Node
		wantErr string
	}{{
		desc:    "nil impl",
		wantErr: "nodeImpl cannot be nil",
	}, {
		desc:    "nil pb",
		wantErr: "nodeImpl.Proto cannot be nil",
		nImpl:   &node.Impl{},
	}, {
		desc: "empty pb",
		nImpl: &node.Impl{
			Proto: &topopb.Node{},
		},
		want: &topopb.Node{
			Config: &topopb.Config{
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
		},
	}, {
		desc: "provided service",
		nImpl: &node.Impl{
			Proto: &topopb.Node{
				Config: &topopb.Config{
					Command: []string{"do", "run"},
				},
				Services: map[uint32]*topopb.Service{
					40051: {
						Name:      "grpc",
						Inside:    40051,
						Outside:   40051,
						InsideIp:  "1.1.1.1",
						OutsideIp: "10.10.10.10",
					},
				},
			},
		},
		want: &topopb.Node{
			Config: &topopb.Config{
				Command:      []string{"do", "run"},
				EntryCommand: fmt.Sprintf("kubectl exec -it %s -- sh", ""),
				Image:        "magna:latest",
			},
			Services: map[uint32]*topopb.Service{
				40051: {
					Name:      "grpc",
					Inside:    40051,
					Outside:   40051,
					InsideIp:  "1.1.1.1",
					OutsideIp: "10.10.10.10",
				},
			},
		},
	}, {
		desc: "provided config command",
		nImpl: &node.Impl{
			Proto: &topopb.Node{
				Config: &topopb.Config{
					Command: []string{"do", "run"},
				},
			},
		},
		want: &topopb.Node{
			Config: &topopb.Config{
				Command:      []string{"do", "run"},
				EntryCommand: fmt.Sprintf("kubectl exec -it %s -- sh", ""),
				Image:        "magna:latest",
			},
		},
	}}
	for _, tt := range tests {
		t.Run(tt.desc, func(t *testing.T) {
			got, err := New(tt.nImpl)
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
