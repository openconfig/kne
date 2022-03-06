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
	"io"
	"io/ioutil"
	"os"
	"testing"
	"time"

	tfake "github.com/google/kne/api/clientset/v1beta1/fake"
	topologyv1 "github.com/google/kne/api/types/v1beta1"
	cpb "github.com/google/kne/proto/controller"
	tpb "github.com/google/kne/proto/topo"
	"github.com/google/kne/topo/node"
	nd "github.com/google/kne/topo/node"
	"github.com/h-fam/errdiff"
	"google.golang.org/protobuf/encoding/prototext"
	"google.golang.org/protobuf/proto"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/util/intstr"
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
  name: "ate1"
  type: IXIA_TG
  services: {
	key: 1000
	value: {
	  name: "gnmi"
	  inside: 1000
	  inside_ip: "1.1.1.1"
	  outside_ip: "100.100.100.100"
	  node_port: 20000
	}
  }
  services: {
	key: 1001
	value: {
	  name: "grpc"
	  inside: 1001
	  inside_ip: "1.1.1.1"
	  outside_ip: "100.100.100.100"
	  node_port: 20001
	}
  }
  services: {
	key: 5555
	value: {
	  name: "port-5555"
	  inside: 5555
	  outside: 5555
	  inside_ip: "1.1.1.3"
	  outside_ip: "100.100.100.102"
	  node_port: 30010
	}
  }
  services: {
	key: 50071
	value: {
	  name: "port-50071"
	  inside: 50071
	  outside: 50071
	  inside_ip: "1.1.1.3"
	  outside_ip: "100.100.100.102"
	  node_port: 30011
	}
  }
  version: "0.0.1-9999"
}
links: {
  a_node: "r1"
  a_int: "eth9"
  z_node: "ate1"
  z_int: "eth1"
}
`
)

// defaultFakeTopology serves as a testing fake with default implementation.
type defaultFakeTopology struct{}

func (f *defaultFakeTopology) Load(context.Context) error {
	return nil
}

func (f *defaultFakeTopology) Topology(context.Context) ([]topologyv1.Topology, error) {
	return nil, nil
}

func (f *defaultFakeTopology) TopologyProto() *tpb.Topology {
	return nil
}

func (f *defaultFakeTopology) Push(context.Context) error {
	return nil
}

func (f *defaultFakeTopology) CheckNodeStatus(context.Context, time.Duration) error {
	return nil
}

func (f *defaultFakeTopology) Delete(context.Context) error {
	return nil
}

func (f *defaultFakeTopology) Nodes() []node.Node {
	return nil
}

func (f *defaultFakeTopology) Resources(context.Context) (*Resources, error) {
	return nil, nil
}

func (f *defaultFakeTopology) Watch(context.Context) error {
	return nil
}

func (f *defaultFakeTopology) ConfigPush(context.Context, string, io.Reader) error {
	return nil
}

func (f *defaultFakeTopology) Node(string) (node.Node, error) {
	return nil, nil
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

// fakeTopology is used to test GetTopologyServices().
type fakeTopology struct {
	defaultFakeTopology
	resources *Resources
	proto     *tpb.Topology
	rErr      error
	lErr      error
}

func (f *fakeTopology) Load(context.Context) error {
	return f.lErr
}

func (f *fakeTopology) TopologyProto() *tpb.Topology {
	return f.proto
}

func (f *fakeTopology) Resources(context.Context) (*Resources, error) {
	return f.resources, f.rErr
}

func TestGetTopologyServices(t *testing.T) {
	tf, err := tfake.NewSimpleClientset()
	if err != nil {
		t.Fatalf("cannot create fake topology clientset")
	}
	opts := []Option{
		WithClusterConfig(&rest.Config{}),
		WithKubeClient(kfake.NewSimpleClientset()),
		WithTopoClient(tf),
	}

	validTopoIn, err := Load("testdata/valid_topo.pb.txt")
	if err != nil {
		t.Fatalf("cannot load the valid topoplogy proto as input: %v", err)
	}
	validTopoOut := &tpb.Topology{}
	if err := prototext.Unmarshal([]byte(validPbTxt), validTopoOut); err != nil {
		t.Fatalf("cannot Unmarshal validTopo: %v", err)
	}
	tests := []struct {
		desc        string
		inputParam  TopologyParams
		topoNewFunc func(string, *tpb.Topology, ...Option) (TopologyManager, error)
		want        *tpb.Topology
		wantErr     string
	}{{
		desc: "load topology error",
		inputParam: TopologyParams{
			TopoName:       "testdata/not_there.pb.txt",
			TopoNewOptions: opts,
		},
		wantErr: "no such file or directory",
	}, {
		desc: "empty resources",
		topoNewFunc: func(string, *tpb.Topology, ...Option) (TopologyManager, error) {
			return &fakeTopology{
				proto:     validTopoIn,
				resources: &Resources{},
			}, nil
		},
		wantErr: "not found",
	}, {
		desc: "load fail",
		topoNewFunc: func(string, *tpb.Topology, ...Option) (TopologyManager, error) {
			return &fakeTopology{
				lErr:      fmt.Errorf("load failed"),
				resources: &Resources{},
			}, nil
		},
		wantErr: "load failed",
	}, {
		desc: "valid case",
		topoNewFunc: func(string, *tpb.Topology, ...Option) (TopologyManager, error) {
			return &fakeTopology{
				proto: validTopoIn,
				resources: &Resources{
					Services: map[string]*corev1.Service{
						"gnmi-service": {
							Spec: corev1.ServiceSpec{
								ClusterIP: "1.1.1.1",
								Ports: []corev1.ServicePort{{
									Port:     1000,
									NodePort: 20000,
									Name:     "gnmi",
								}},
							},
							Status: corev1.ServiceStatus{
								LoadBalancer: corev1.LoadBalancerStatus{
									Ingress: []corev1.LoadBalancerIngress{{IP: "100.100.100.100"}},
								},
							},
						},
						"grpc-service": {
							Spec: corev1.ServiceSpec{
								ClusterIP: "1.1.1.1",
								Ports: []corev1.ServicePort{{
									Port:     1001,
									NodePort: 20001,
									Name:     "grpc",
								}},
							},
							Status: corev1.ServiceStatus{
								LoadBalancer: corev1.LoadBalancerStatus{
									Ingress: []corev1.LoadBalancerIngress{{IP: "100.100.100.100"}},
								},
							},
						},
						"service-r1": {
							Spec: corev1.ServiceSpec{
								ClusterIP: "1.1.1.2",
								Ports: []corev1.ServicePort{{
									Port:       1002,
									NodePort:   22,
									Name:       "ssh",
									TargetPort: intstr.IntOrString{IntVal: 22},
								}},
							},
							Status: corev1.ServiceStatus{
								LoadBalancer: corev1.LoadBalancerStatus{
									Ingress: []corev1.LoadBalancerIngress{{IP: "100.100.100.101"}},
								},
							},
						},
						"service-ate1": {
							Spec: corev1.ServiceSpec{
								ClusterIP: "1.1.1.3",
								Ports: []corev1.ServicePort{{
									Port:       5555,
									NodePort:   30010,
									Name:       "port-5555",
									TargetPort: intstr.IntOrString{IntVal: 5555},
								}, {
									Port:       50071,
									NodePort:   30011,
									Name:       "port-50071",
									TargetPort: intstr.IntOrString{IntVal: 50071},
								}},
							},
							Status: corev1.ServiceStatus{
								LoadBalancer: corev1.LoadBalancerStatus{
									Ingress: []corev1.LoadBalancerIngress{{IP: "100.100.100.102"}},
								},
							},
						},
					},
				},
			}, nil
		},
		wantErr: "",
		want:    validTopoOut,
	},
	}

	for _, tc := range tests {
		t.Run(tc.desc, func(t *testing.T) {
			if tc.topoNewFunc != nil {
				origNew := new
				new = tc.topoNewFunc
				defer func() {
					new = origNew
				}()
			}
			got, err := GetTopologyServices(context.Background(), tc.inputParam)
			if diff := errdiff.Check(err, tc.wantErr); diff != "" {
				t.Fatalf("failed: %+v", err)
			}
			if tc.wantErr != "" {
				return
			}
			if !proto.Equal(got.Topology, tc.want) {
				t.Fatalf("get topology service failed: got:\n%s\n, want:\n%s\n", got, tc.want)
			}
		})
	}
}

func TestStateMap(t *testing.T) {
	type node struct {
		name  string
		phase nd.NodeStatus
	}

	tests := []struct {
		desc  string
		nodes []*node
		want  cpb.TopologyState
	}{{
		desc: "no nodes",
		want: cpb.TopologyState_TOPOLOGY_STATE_UNKNOWN,
	}, {
		desc: "one node failed",
		nodes: []*node{
			{"n1", nd.NODE_FAILED},
			{"n2", nd.NODE_RUNNING},
			{"n3", nd.NODE_RUNNING},
		},
		want: cpb.TopologyState_TOPOLOGY_STATE_ERROR,
	}, {
		desc: "one node failed with one node pending",
		nodes: []*node{
			{"n1", nd.NODE_FAILED},
			{"n2", nd.NODE_PENDING},
			{"n3", nd.NODE_RUNNING},
		},
		want: cpb.TopologyState_TOPOLOGY_STATE_ERROR,
	}, {
		desc: "one node failed, one node pending, one node unknown",
		nodes: []*node{
			{"n1", nd.NODE_FAILED},
			{"n2", nd.NODE_PENDING},
			{"n3", nd.NODE_UNKNOWN},
		},
		want: cpb.TopologyState_TOPOLOGY_STATE_ERROR,
	}, {
		desc: "all nodes failed",
		nodes: []*node{
			{"n1", nd.NODE_FAILED},
			{"n2", nd.NODE_FAILED},
			{"n3", nd.NODE_FAILED},
		},
		want: cpb.TopologyState_TOPOLOGY_STATE_ERROR,
	}, {
		desc: "one node pending",
		nodes: []*node{
			{"n1", nd.NODE_PENDING},
			{"n2", nd.NODE_RUNNING},
			{"n3", nd.NODE_RUNNING},
		},
		want: cpb.TopologyState_TOPOLOGY_STATE_CREATING,
	},
	}

	for _, tc := range tests {
		t.Run(tc.desc, func(t *testing.T) {
			sm := &sMap{}
			for _, n := range tc.nodes {
				sm.SetNodeState(n.name, n.phase)
			}
			got := sm.TopoState()
			if tc.want != got {
				t.Fatalf("want: %+v, got: %+v", tc.want, got)
			}
		})
	}
}
