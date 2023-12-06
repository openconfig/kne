// Copyright 2021 Google LLC
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//	http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.
package alpine

import (
	"fmt"
	"testing"

	"github.com/h-fam/errdiff"
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
		desc: "empty pb",
		nImpl: &node.Impl{
			Proto: &tpb.Node{},
		},
		want: &tpb.Node{
			Config: &tpb.Config{
				Command:      []string{"/bin/sh", "-c", "sleep 2000000000000"},
				EntryCommand: fmt.Sprintf("kubectl exec -it %s -- sh", ""),
				Image:        "sonic-vs:latest",
			},
		},
	}, {
		desc: "provided alpine container",
		nImpl: &node.Impl{
			Proto: &tpb.Node{
				Config: &tpb.Config{
					Image: "alpine-vs:latest",
					Command: []string{"go", "run", "main"},
				},
			},
		},
		want: &tpb.Node{
			Config: &tpb.Config{
				Image: "alpine-vs:latest",
				Command: []string{"go", "run", "main"},
				EntryCommand: fmt.Sprintf("kubectl exec -it %s -- sh", ""),
			},
		},
	}, 
	}
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