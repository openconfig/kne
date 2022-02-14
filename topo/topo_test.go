// Copyright 2021 Google LLC
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//      http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package topo

import (
	"context"
	"fmt"
	"testing"

	tfake "github.com/google/kne/api/clientset/v1beta1/fake"
	tpb "github.com/google/kne/proto/topo"
	"github.com/h-fam/errdiff"
	kfake "k8s.io/client-go/kubernetes/fake"
	"k8s.io/client-go/rest"
)

func TestCreateTopology(t *testing.T) {
	tf, err := tfake.NewSimpleClientset()
	if err != nil {
		t.Fatalf("cannot create fake topology clientset")
	}
	opts := []Option{
		WithClusterConfig(&rest.Config{}),
		WithKubeClient(kfake.NewSimpleClientset()),
		WithTopoClient(tf),
	}

	tests := []struct {
		desc       string
		inputParam TopologyParams
		wantErr    string
	}{{
		desc: "create with valid topology file",
		inputParam: TopologyParams{
			TopoName:       "testdata/valid_topo.pb.txt",
			TopoNewFunc:    New,
			TopoNewOptions: opts,
			DryRun:         true,
		},
		wantErr: "",
	}, {
		desc: "create with non-existent topology file",
		inputParam: TopologyParams{
			TopoName:       "testdata/non_existing.pb.txt",
			TopoNewFunc:    New,
			TopoNewOptions: opts,
			DryRun:         true,
		},
		wantErr: "no such file or directory",
	}, {
		desc: "create with invalid topology",
		inputParam: TopologyParams{
			TopoName: "testdata/valid_topo.pb.txt",
			TopoNewFunc: func(string, *tpb.Topology, ...Option) (*Manager, error) {
				return nil, fmt.Errorf("invalid topology")
			},
			TopoNewOptions: opts,
			DryRun:         true,
		},
		wantErr: "invalid topology",
	},
	}

	for _, tc := range tests {
		t.Run(tc.desc, func(t *testing.T) {
			err := CreateTopology(context.Background(), tc.inputParam)
			if diff := errdiff.Check(err, tc.wantErr); diff != "" {
				t.Fatalf("failed: %+v", err)
			}
		})
	}
}

func TestDeleteTopology(t *testing.T) {
	tf, err := tfake.NewSimpleClientset()
	if err != nil {
		t.Fatalf("cannot create fake topology clientset")
	}
	opts := []Option{
		WithClusterConfig(&rest.Config{}),
		WithKubeClient(kfake.NewSimpleClientset()),
		WithTopoClient(tf),
	}

	tests := []struct {
		desc       string
		inputParam TopologyParams
		wantErr    string
	}{{
		desc: "delete a non-existing topology with valid topology file",
		inputParam: TopologyParams{
			TopoName:       "testdata/valid_topo.pb.txt",
			TopoNewFunc:    New,
			TopoNewOptions: opts,
			DryRun:         true,
		},
		wantErr: "does not exist in cluster",
	}, {
		desc: "delete with non-existent topology file",
		inputParam: TopologyParams{
			TopoName:       "testdata/non_existing.pb.txt",
			TopoNewFunc:    New,
			TopoNewOptions: opts,
			DryRun:         true,
		},
		wantErr: "no such file or directory",
	}, {
		desc: "delete with invalid topology",
		inputParam: TopologyParams{
			TopoName: "testdata/valid_topo.pb.txt",
			TopoNewFunc: func(string, *tpb.Topology, ...Option) (*Manager, error) {
				return nil, fmt.Errorf("invalid topology")
			},
			TopoNewOptions: opts,
			DryRun:         true,
		},
		wantErr: "invalid topology",
	},
	}

	for _, tc := range tests {
		t.Run(tc.desc, func(t *testing.T) {
			err := DeleteTopology(context.Background(), tc.inputParam)
			if diff := errdiff.Check(err, tc.wantErr); diff != "" {
				t.Fatalf("failed: %+v", err)
			}
		})
	}
}
