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
	"google.golang.org/protobuf/encoding/prototext"
	"google.golang.org/protobuf/proto"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/util/intstr"
	kfake "k8s.io/client-go/kubernetes/fake"
	"k8s.io/client-go/rest"
)

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
	validTopoOut := &tpb.Topology{}
	if err := prototext.Unmarshal([]byte(validPbTxt), validTopoOut); err != nil {
		t.Fatalf("cannot Unmarshal validTopo: %v", err)
	}
	tests := []struct {
		desc       string
		inputParam TopologyParams
		want       *tpb.Topology
		wantErr    string
	}{{
		desc: "load topology error",
		inputParam: TopologyParams{
			TopoName:       "testdata/not_there.pb.txt",
			TopoNewOptions: opts,
			fakeTopo: &fakeTopology{
				resources: &Resources{},
			},
		},
		wantErr: "no such file or directory",
	}, {
		desc: "empty resources",
		inputParam: TopologyParams{
			TopoName:       "testdata/valid_topo.pb.txt",
			TopoNewOptions: opts,
			fakeTopo: &fakeTopology{
				resources: &Resources{},
			},
		},
		wantErr: "not found",
	}, {
		desc: "load fail",
		inputParam: TopologyParams{
			TopoName:       "testdata/valid_topo.pb.txt",
			TopoNewOptions: opts,
			fakeTopo: &fakeTopology{
				lErr:      fmt.Errorf("load failed"),
				resources: &Resources{},
			},
		},
		wantErr: "load failed",
	}, {
		desc: "valid case",
		inputParam: TopologyParams{
			TopoName:       "testdata/valid_topo.pb.txt",
			TopoNewOptions: opts,
			fakeTopo: &fakeTopology{
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
			},
		},
		wantErr: "",
		want:    validTopoOut,
	},
	}

	for _, tc := range tests {
		t.Run(tc.desc, func(t *testing.T) {
			got, err := GetTopologyServices(context.Background(), tc.inputParam)
			if diff := errdiff.Check(err, tc.wantErr); diff != "" {
				t.Fatalf("failed: %+v", err)
			}
			if tc.wantErr != "" {
				return
			}
			if !proto.Equal(got, tc.want) {
				t.Fatalf("get topology service failed: got:\n%s\n, want:\n%s\n", got, tc.want)
			}
		})
	}
}
