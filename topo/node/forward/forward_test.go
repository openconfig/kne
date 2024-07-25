package forward

import (
	"fmt"
	"testing"

	"github.com/openconfig/gnmi/errdiff"
	topopb "github.com/openconfig/kne/proto/topo"
	"github.com/openconfig/kne/topo/node"
	"google.golang.org/protobuf/encoding/prototext"
	"google.golang.org/protobuf/proto"
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
				EntryCommand: fmt.Sprintf("kubectl exec -it %s -- sh", ""),
				Image:        "forward:latest",
				ConfigPath:   "/etc",
				ConfigFile:   "config",
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
					2000: {
						Name:      "Service",
						Inside:    2000,
						Outside:   20001,
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
				Image:        "forward:latest",
				ConfigPath:   "/etc",
				ConfigFile:   "config",
			},
			Services: map[uint32]*topopb.Service{
				2000: {
					Name:      "Service",
					Inside:    2000,
					Outside:   20001,
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
				Image:        "forward:latest",
				ConfigPath:   "/etc",
				ConfigFile:   "config",
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
