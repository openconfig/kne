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
	"io/ioutil"
	"os"
	"testing"

	tfake "github.com/google/kne/api/clientset/v1beta1/fake"
	"github.com/h-fam/errdiff"
	kfake "k8s.io/client-go/kubernetes/fake"
	"k8s.io/client-go/rest"
)

func TestLoad(t *testing.T) {
	type args struct {
		fName string
	}

	invalidPb, err := ioutil.TempFile(".", "invalid*.pb.txt")
	if err != nil {
		t.Errorf("failed creating tmp pb: %v", err)
	}
	defer os.Remove(invalidPb.Name())

	invalidYaml, err := ioutil.TempFile(".", "invalid*.yaml")
	if err != nil {
		t.Errorf("failed creating tmp yaml: %v", err)
	}
	defer os.Remove(invalidYaml.Name())

	invalidPb.WriteString(`
	name: "2node-ixia"
	nodes: {
		nme: "ixia-c-port1"
	}
	`)

	invalidYaml.WriteString(`
	name: 2node-ixia
	nodes:
	  - name: ixia-c-port1
	`)

	tests := []struct {
		name    string
		args    args
		wantErr bool
	}{
		{name: "pb", args: args{fName: "../examples/2node-ixia.pb.txt"}, wantErr: false},
		{name: "yaml", args: args{fName: "../examples/2node-ixia.yaml"}, wantErr: false},
		{name: "invalid-pb", args: args{fName: invalidPb.Name()}, wantErr: true},
		{name: "invalid-yaml", args: args{fName: invalidYaml.Name()}, wantErr: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := Load(tt.args.fName)
			if (err != nil) != tt.wantErr {
				t.Errorf("Load() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
		})
	}
}

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
			TopoNewOptions: opts,
			DryRun:         true,
		},
		wantErr: "",
	}, {
		desc: "create with non-existent topology file",
		inputParam: TopologyParams{
			TopoName:       "testdata/non_existing.pb.txt",
			TopoNewOptions: opts,
			DryRun:         true,
		},
		wantErr: "no such file or directory",
	}, {
		desc: "create with invalid topology",
		inputParam: TopologyParams{
			TopoName:       "testdata/invalid_topo.pb.txt",
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
			TopoNewOptions: opts,
			DryRun:         true,
		},
		wantErr: "does not exist in cluster",
	}, {
		desc: "delete with non-existent topology file",
		inputParam: TopologyParams{
			TopoName:       "testdata/non_existing.pb.txt",
			TopoNewOptions: opts,
			DryRun:         true,
		},
		wantErr: "no such file or directory",
	}, {
		desc: "delete with invalid topology",
		inputParam: TopologyParams{
			TopoName:       "testdata/invalid_topo.pb.txt",
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
