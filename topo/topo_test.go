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
	"bytes"
	"context"
	"fmt"
	"io"
	"os"
	"testing"

	"github.com/google/go-cmp/cmp"
	"github.com/h-fam/errdiff"
	// tfake "github.com/openconfig/kne/api/clientset/v1beta1/fake"
	// topologyv1 "github.com/openconfig/kne/api/types/v1beta1"
	cpb "github.com/openconfig/kne/proto/controller"
	tpb "github.com/openconfig/kne/proto/topo"
	"github.com/openconfig/kne/topo/node"
	// "google.golang.org/protobuf/encoding/prototext"
	// "google.golang.org/protobuf/proto"
	// corev1 "k8s.io/api/core/v1"
	// "k8s.io/apimachinery/pkg/util/intstr"
	// kfake "k8s.io/client-go/kubernetes/fake"
	// "k8s.io/client-go/rest"
)

func TestLoad(t *testing.T) {
	invalidPb, err := os.CreateTemp(".", "invalid*.pb.txt")
	if err != nil {
		t.Errorf("failed creating tmp pb: %v", err)
	}
	defer os.Remove(invalidPb.Name())

	invalidYaml, err := os.CreateTemp(".", "invalid*.yaml")
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
		desc    string
		path    string
		wantErr bool
	}{{
		desc: "pb",
		path: "../examples/2node-ixia-ceos.pb.txt",
	}, {
		desc: "yaml",
		path: "../examples/2node-ixia-ceos.yaml",
	}, {
		desc:    "invalid-pb",
		path:    invalidPb.Name(),
		wantErr: true,
	}, {
		desc:    "invalid-yaml",
		path:    invalidYaml.Name(),
		wantErr: true,
	}}
	for _, tt := range tests {
		t.Run(tt.desc, func(t *testing.T) {
			_, err := Load(tt.path)
			if (err != nil) != tt.wantErr {
				t.Errorf("Load() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

var (
	validPbTxt = `
name: "test-data-topology"
nodes: {
  name: "r1"
  type: ARISTA_CEOS
  services: {
	key: 1002
	value: {
  	  name: "ssh"
	  inside: 1002
	  outside: 22
	  inside_ip: "1.1.1.2"
	  outside_ip: "100.100.100.101"
	  node_port: 22
	}
  }
}
nodes: {
    name: "otg"
    type: IXIA_TG
    version: "0.0.1-9999"
    services: {
        key: 40051
        value: {
            name: "grpc"
            inside: 40051
			inside_ip: "1.1.1.1"
			outside_ip: "100.100.100.100"
			node_port: 20001
        }
    }
    services: {
        key: 50051
        value: {
            name: "gnmi"
            inside: 50051
			inside_ip: "1.1.1.1"
			outside_ip: "100.100.100.100"
			node_port: 20000
        }
    }
}
links: {
  a_node: "r1"
  a_int: "eth9"
  z_node: "otg"
  z_int: "eth1"
}
`
)

func TestNew(t *testing.T) {} // TODO

func TestCreate(t *testing.T) {} // TODO

func TestDelete(t *testing.T) {} // TODO

func TestShow(t *testing.T) {} // TODO

func TestWatch(t *testing.T) {} // TODO

func TestResources(t *testing.T) {} // TODO

func TestNodes(t *testing.T) {
	aNode := &configurable{}
	bNode := &configurable{}
	cNode := &configurable{}
	tests := []struct {
		desc  string
		nodes map[string]node.Node
		want  []node.Node
	}{{
		desc: "non-zero nodes",
		nodes: map[string]node.Node{
			"a": aNode,
			"b": bNode,
			"c": cNode,
		},
		want: []node.Node{
			aNode,
			bNode,
			cNode,
		},
	}, {
		desc: "zero nodes",
	}}
	for _, tt := range tests {
		t.Run(tt.desc, func(t *testing.T) {
			m := &Manager{nodes: tt.nodes}
			got := m.Nodes()
			if s := cmp.Diff(got, tt.want); s != "" {
				t.Errorf("Nodes() unexpected diff: %s", s)
			}
		})
	}
}

type configurable struct {
	*node.Impl
}

func (c *configurable) ConfigPush(_ context.Context, r io.Reader) error {
	b, err := io.ReadAll(r)
	if err != nil {
		return err
	}
	if string(b) == "error" {
		return fmt.Errorf("error")
	}
	return nil
}

type notConfigurable struct {
	*node.Impl
}

func TestConfigPush(t *testing.T) {
	m := &Manager{
		nodes: map[string]node.Node{
			"configurable":     &configurable{},
			"not_configurable": &notConfigurable{},
		},
	}
	tests := []struct {
		desc    string
		name    string
		cfg     io.Reader
		wantErr string
	}{{
		desc: "configurable good config",
		name: "configurable",
		cfg:  bytes.NewReader([]byte("good config")),
	}, {
		desc:    "configurable bad config",
		name:    "configurable",
		cfg:     bytes.NewReader([]byte("error")),
		wantErr: "error",
	}, {
		desc:    "not configurable",
		name:    "not_configurable",
		wantErr: "does not implement ConfigPusher interface",
	}, {
		desc:    "node not found",
		name:    "dne",
		wantErr: "not found",
	}}
	for _, tt := range tests {
		t.Run(tt.desc, func(t *testing.T) {
			err := m.ConfigPush(context.Background(), tt.name, tt.cfg)
			if s := errdiff.Check(err, tt.wantErr); s != "" {
				t.Errorf("ConfigPush() unexpected error: %s", s)
			}
		})
	}
}

type resettable struct {
	*node.Impl
	rErr string
}

func (r *resettable) ResetCfg(_ context.Context) error {
	if r.rErr != "" {
		return fmt.Errorf(r.rErr)
	}
	return nil
}

type notResettable struct {
	*node.Impl
}

func TestResetCfg(t *testing.T) {
	m := &Manager{
		nodes: map[string]node.Node{
			"resettable":     &resettable{},
			"resettable_err": &resettable{rErr: "failed to reset"},
			"not_resettable": &notResettable{},
		},
	}
	tests := []struct {
		desc    string
		name    string
		wantErr string
	}{{
		desc: "resettable",
		name: "resettable",
	}, {
		desc:    "resettable failure",
		name:    "resettable_err",
		wantErr: "failed to reset",
	}, {
		desc:    "not resettable",
		name:    "not_resettable",
		wantErr: "does not implement Resetter interface",
	}, {
		desc:    "node not found",
		name:    "dne",
		wantErr: "not found",
	}}
	for _, tt := range tests {
		t.Run(tt.desc, func(t *testing.T) {
			err := m.ResetCfg(context.Background(), tt.name)
			if s := errdiff.Check(err, tt.wantErr); s != "" {
				t.Errorf("ResetCfg() unexpected error: %s", s)
			}
		})
	}
}

type certable struct {
	*node.Impl
	proto *tpb.Node
	gErr  string
}

func (c *certable) GetProto() *tpb.Node {
	return c.proto
}

func (c *certable) GenerateSelfSigned(_ context.Context) error {
	if c.gErr != "" {
		return fmt.Errorf(c.gErr)
	}
	return nil
}

type notCertable struct {
	*node.Impl
	proto *tpb.Node
}

func (nc *notCertable) GetProto() *tpb.Node {
	return nc.proto
}

func TestGenerateSelfSigned(t *testing.T) {
	m := &Manager{
		nodes: map[string]node.Node{
			"certable": &certable{
				proto: &tpb.Node{
					Config: &tpb.Config{
						Cert: &tpb.CertificateCfg{
							Config: &tpb.CertificateCfg_SelfSigned{},
						},
					},
				},
			},
			"certable_err": &certable{
				gErr: "failed to generate certs",
				proto: &tpb.Node{
					Config: &tpb.Config{
						Cert: &tpb.CertificateCfg{
							Config: &tpb.CertificateCfg_SelfSigned{},
						},
					},
				},
			},
			"not_certable": &notCertable{
				proto: &tpb.Node{
					Config: &tpb.Config{
						Cert: &tpb.CertificateCfg{
							Config: &tpb.CertificateCfg_SelfSigned{},
						},
					},
				},
			},
			"no_info": &certable{},
		},
	}
	tests := []struct {
		desc    string
		name    string
		wantErr string
	}{{
		desc: "certable",
		name: "certable",
	}, {
		desc:    "certable failure",
		name:    "certable_err",
		wantErr: "failed to generate certs",
	}, {
		desc:    "not certable",
		name:    "not_certable",
		wantErr: "does not implement Certer interface",
	}, {
		desc: "no cert info",
		name: "no_info",
	}, {
		desc:    "node not found",
		name:    "dne",
		wantErr: "not found",
	}}
	for _, tt := range tests {
		t.Run(tt.desc, func(t *testing.T) {
			err := m.GenerateSelfSigned(context.Background(), tt.name)
			if s := errdiff.Check(err, tt.wantErr); s != "" {
				t.Errorf("GenerateSelfSigned() unexpected error: %s", s)
			}
		})
	}
}

func TestStateMap(t *testing.T) {
	type nodeInfo struct {
		name  string
		phase node.Status
	}

	tests := []struct {
		desc  string
		nodes []*nodeInfo
		want  cpb.TopologyState
	}{{
		desc: "no nodes",
		want: cpb.TopologyState_TOPOLOGY_STATE_UNSPECIFIED,
	}, {
		desc: "one node failed",
		nodes: []*nodeInfo{
			{"n1", node.StatusFailed},
			{"n2", node.StatusRunning},
			{"n3", node.StatusRunning},
		},
		want: cpb.TopologyState_TOPOLOGY_STATE_ERROR,
	}, {
		desc: "one node failed with one node pending",
		nodes: []*nodeInfo{
			{"n1", node.StatusFailed},
			{"n2", node.StatusRunning},
			{"n3", node.StatusRunning},
		},
		want: cpb.TopologyState_TOPOLOGY_STATE_ERROR,
	}, {
		desc: "one node failed, one node pending, one node unknown",
		nodes: []*nodeInfo{
			{"n1", node.StatusFailed},
			{"n2", node.StatusPending},
			{"n3", node.StatusUnknown},
		},
		want: cpb.TopologyState_TOPOLOGY_STATE_ERROR,
	}, {
		desc: "all nodes failed",
		nodes: []*nodeInfo{
			{"n1", node.StatusFailed},
			{"n2", node.StatusFailed},
			{"n3", node.StatusFailed},
		},
		want: cpb.TopologyState_TOPOLOGY_STATE_ERROR,
	}, {
		desc: "one node pending",
		nodes: []*nodeInfo{
			{"n1", node.StatusPending},
			{"n2", node.StatusRunning},
			{"n3", node.StatusRunning},
		},
		want: cpb.TopologyState_TOPOLOGY_STATE_CREATING,
	},
	}

	for _, tc := range tests {
		t.Run(tc.desc, func(t *testing.T) {
			sm := &stateMap{}
			for _, n := range tc.nodes {
				sm.setNodeState(n.name, n.phase)
			}
			got := sm.topologyState()
			if tc.want != got {
				t.Fatalf("want: %+v, got: %+v", tc.want, got)
			}
		})
	}
}
