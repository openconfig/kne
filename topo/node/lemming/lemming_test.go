// Copyright 2022 Google LLC
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

package lemming

import (
	"testing"

	"github.com/h-fam/errdiff"
	"github.com/openconfig/kne/topo/node"
	"google.golang.org/protobuf/encoding/prototext"
	"google.golang.org/protobuf/proto"

	tpb "github.com/openconfig/kne/proto/topo"
)

func TestNew(t *testing.T) {
	tests := []struct {
		desc    string
		ni      *node.Impl
		wantPB  *tpb.Node
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
			Proto: &tpb.Node{
				Name:   "test_node",
				Config: &tpb.Config{},
			},
		},
		wantPB: &tpb.Node{
			Name: "test_node",
			Config: &tpb.Config{
				Image:        "lemming:latest",
				Command:      []string{"/lemming/lemming"},
				Args:         []string{"--alsologtostderr"},
				EntryCommand: "kubectl exec -it test_node -- /bin/bash",
			},
			Labels: map[string]string{
				"vendor": tpb.Vendor_OPENCONFIG.String(),
			},
			Constraints: map[string]string{
				"cpu":    "0.5",
				"memory": "1Gi",
			},
			Services: map[uint32]*tpb.Service{
				6030: {
					Name:    "gnmi",
					Inside:  6030,
					Outside: 6030,
				},
				6031: {
					Name:    "gribi",
					Inside:  6030,
					Outside: 6031,
				},
			},
		},
	}, {
		desc: "defaults not overriding",
		ni: &node.Impl{
			Proto: &tpb.Node{
				Name: "test_node",
				Config: &tpb.Config{
					Image:        "lemming:test",
					Command:      []string{"/lemming/lemming2"},
					Args:         []string{"-v=2"},
					EntryCommand: "kubectl exec -it test_node -- /bin/sh",
				},
				Constraints: map[string]string{
					"cpu": "10",
				},
				Labels: map[string]string{
					"custom": "value",
				},
				Services: map[uint32]*tpb.Service{
					8080: {
						Name:   "gnmi",
						Inside: 8080,
					},
				},
			},
		},
		wantPB: &tpb.Node{
			Name: "test_node",
			Config: &tpb.Config{
				Image:        "lemming:test",
				Command:      []string{"/lemming/lemming2"},
				Args:         []string{"-v=2"},
				EntryCommand: "kubectl exec -it test_node -- /bin/sh",
			},
			Constraints: map[string]string{
				"cpu": "10",
			},
			Labels: map[string]string{
				"custom": "value",
			},
			Services: map[uint32]*tpb.Service{
				8080: {
					Name:   "gnmi",
					Inside: 8080,
				},
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
