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

package openconfig

import (
	"context"
	"fmt"
	"testing"

	"github.com/google/go-cmp/cmp"
	"github.com/openconfig/gnmi/errdiff"
	tpb "github.com/openconfig/kne/proto/topo"
	"github.com/openconfig/kne/topo/node"
	"github.com/openconfig/lemming/operator/api/clientset"
	"github.com/openconfig/lemming/operator/api/clientset/fake"
	lemmingv1 "github.com/openconfig/lemming/operator/api/lemming/v1alpha1"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/testing/protocmp"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/rest"
	k8stesting "k8s.io/client-go/testing"
)

func TestCreate(t *testing.T) {
	tests := []struct {
		desc        string
		n           *Node
		clientFnErr error
		createErr   error
		wantErr     string
		want        *lemmingv1.Lemming
	}{{
		desc: "lemming: error creating client set",
		n: &Node{
			Impl: &node.Impl{
				Proto: &tpb.Node{
					Model: modelLemming,
					Config: &tpb.Config{
						Command: []string{"/lemming"},
					},
				},
			},
		},
		clientFnErr: fmt.Errorf("client err"),
		wantErr:     "client err",
	}, {
		desc: "lemming: create error",
		n: &Node{
			Impl: &node.Impl{
				Proto: &tpb.Node{
					Model: modelLemming,
					Config: &tpb.Config{
						Command: []string{"/lemming"},
					},
				},
			},
		},
		createErr: fmt.Errorf("create err"),
		wantErr:   "create err",
	}, {
		desc: "lemming: success",
		n: &Node{
			Impl: &node.Impl{
				Namespace: "default",
				Proto: &tpb.Node{
					Name:  "test",
					Model: modelLemming,
					Config: &tpb.Config{
						Command: []string{"/lemming"},
						Cert: &tpb.CertificateCfg{
							Config: &tpb.CertificateCfg_SelfSigned{
								SelfSigned: &tpb.SelfSignedCertCfg{
									CommonName: "foo",
								},
							},
						},
					},
					Services: map[uint32]*tpb.Service{
						9339: {
							Inside:  9339,
							Outside: 9339,
							Name:    "gnmi",
						},
					},
				},
			},
		},
		want: &lemmingv1.Lemming{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "test",
				Namespace: "default",
			},
			Spec: lemmingv1.LemmingSpec{
				Command:        "/lemming",
				Ports:          map[string]lemmingv1.ServicePort{"gnmi": {InnerPort: 9339, OuterPort: 9339}},
				InterfaceCount: 1,
				Resources: corev1.ResourceRequirements{
					Requests: corev1.ResourceList{},
				},
				TLS: &lemmingv1.TLSSpec{
					SelfSigned: &lemmingv1.SelfSignedSpec{
						CommonName: "foo",
					},
				},
			},
		},
	}}
	for _, tt := range tests {
		t.Run(tt.desc, func(t *testing.T) {
			cs := fake.NewSimpleClientset()
			cs.PrependReactor("create", "lemmings", func(action k8stesting.Action) (handled bool, ret runtime.Object, err error) {
				return tt.createErr != nil, nil, tt.createErr
			})
			clientFn = func(c *rest.Config) (clientset.Interface, error) {
				return cs, tt.clientFnErr
			}

			err := tt.n.Create(context.Background())
			if s := errdiff.Substring(err, tt.wantErr); s != "" {
				t.Fatalf("unexpected error: got: %v, want: %s", err, s)
			}
			if tt.wantErr != "" {
				return
			}
			got, err := cs.LemmingV1alpha1().Lemmings("default").Get(context.Background(), "test", metav1.GetOptions{})
			if err != nil {
				t.Fatal(err)
			}
			if d := cmp.Diff(tt.want, got); d != "" {
				t.Errorf("unexpected diff (-want +got):\n%s", d)
			}
		})
	}
}

func TestLemmingStatus(t *testing.T) {
	tests := []struct {
		desc        string
		clientFnErr error
		getErr      error
		inPhase     lemmingv1.LemmingPhase
		wantErr     string
		want        node.Status
	}{{
		desc:        "error creating client set",
		clientFnErr: fmt.Errorf("client err"),
		wantErr:     "client err",
	}, {
		desc:    "get error",
		getErr:  fmt.Errorf("create err"),
		wantErr: "create err",
	}, {
		desc:    "running",
		want:    node.StatusRunning,
		inPhase: lemmingv1.Running,
	}, {
		desc:    "running",
		want:    node.StatusRunning,
		inPhase: lemmingv1.Running,
	}, {
		desc:    "failed",
		want:    node.StatusFailed,
		inPhase: lemmingv1.Failed,
	}, {
		desc:    "pending",
		want:    node.StatusPending,
		inPhase: lemmingv1.Pending,
	}, {
		desc:    "unknown",
		want:    node.StatusUnknown,
		inPhase: lemmingv1.Unknown,
	}}
	for _, tt := range tests {
		t.Run(tt.desc, func(t *testing.T) {
			cs := fake.NewSimpleClientset()
			cs.PrependReactor("get", "lemmings", func(action k8stesting.Action) (handled bool, ret runtime.Object, err error) {
				return true, &lemmingv1.Lemming{
					Status: lemmingv1.LemmingStatus{
						Phase: tt.inPhase,
					},
				}, tt.getErr
			})
			clientFn = func(c *rest.Config) (clientset.Interface, error) {
				return cs, tt.clientFnErr
			}
			n := &Node{&node.Impl{Proto: &tpb.Node{}}}
			got, err := n.lemmingStatus(context.Background())
			if s := errdiff.Substring(err, tt.wantErr); s != "" {
				t.Fatalf("Status() unexpected error: got: %v, want: %s", err, s)
			}
			if tt.wantErr != "" {
				return
			}
			if got != tt.want {
				t.Errorf("Status() unexpected result: got %v, want %v", got, tt.want)
			}
		})
	}
}

func TestResetCfg(t *testing.T) {
	n := &Node{}
	err := n.ResetCfg(context.Background())
	if err != nil {
		t.Fatalf("ResetCfg() unexpected error: %v", err)
	}
}

func TestConfigPush(t *testing.T) {
	n := &Node{}
	err := n.ConfigPush(context.Background(), nil)
	want := codes.Unimplemented
	if s, ok := status.FromError(err); !ok || s.Code() != want {
		t.Fatalf("ConfigPush() unexpected error get %v, want %v", s, want)
	}
}

func TestGenerateSelfSigned(t *testing.T) {
	n := &Node{}
	err := n.GenerateSelfSigned(context.Background())
	want := codes.Unimplemented
	if s, ok := status.FromError(err); !ok || s.Code() != want {
		t.Fatalf("GenerateSelfSigned() unexpected error get %v, want %v", s, want)
	}
}

func TestLemmingDelete(t *testing.T) {
	tests := []struct {
		desc        string
		clientFnErr error
		deleteErr   error
		wantErr     string
		want        node.Status
	}{{
		desc:        "error creating client set",
		clientFnErr: fmt.Errorf("client err"),
		wantErr:     "client err",
	}, {
		desc:      "delete error",
		deleteErr: fmt.Errorf("delete err"),
		wantErr:   "delete err",
	}, {
		desc: "success",
	}}
	for _, tt := range tests {
		t.Run(tt.desc, func(t *testing.T) {
			cs := fake.NewSimpleClientset()
			cs.PrependReactor("delete", "lemmings", func(action k8stesting.Action) (handled bool, ret runtime.Object, err error) {
				return true, nil, tt.deleteErr
			})
			clientFn = func(c *rest.Config) (clientset.Interface, error) {
				return cs, tt.clientFnErr
			}
			n := &Node{&node.Impl{Proto: &tpb.Node{}}}
			err := n.lemmingDelete(context.Background())
			if s := errdiff.Substring(err, tt.wantErr); s != "" {
				t.Fatalf("lemmingDelete() unexpected error: got: %v, want: %s", err, s)
			}
		})
	}
}

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
		desc: "no node model specified",
		ni: &node.Impl{
			Proto: &tpb.Node{
				Name: "foo",
			},
		},
		wantErr: "a model must be specified",
	}, {
		desc: "lemming: test defaults",
		ni: &node.Impl{
			Proto: &tpb.Node{
				Name:  "test_node",
				Model: modelLemming,
			},
		},
		wantPB: &tpb.Node{
			Name:  "test_node",
			Model: modelLemming,
			Config: &tpb.Config{
				Image:        "us-west1-docker.pkg.dev/openconfig-lemming/release/lemming:ga",
				InitImage:    node.DefaultInitContainerImage,
				Command:      []string{"/lemming/lemming"},
				EntryCommand: "kubectl exec -it test_node -- /bin/bash",
				Cert: &tpb.CertificateCfg{
					Config: &tpb.CertificateCfg_SelfSigned{
						SelfSigned: &tpb.SelfSignedCertCfg{
							CommonName: "test_node",
							KeySize:    2048,
						},
					},
				},
			},
			Labels: map[string]string{
				"vendor":       tpb.Vendor_OPENCONFIG.String(),
				"ondatra-role": "DUT",
			},
			Constraints: map[string]string{
				"cpu":    "500m",
				"memory": "1Gi",
			},
			Services: map[uint32]*tpb.Service{
				9339: {
					Names:  []string{"gnmi", "gnoi", "gnsi"},
					Inside: 9339,
				},
				9340: {
					Names:  []string{"gribi"},
					Inside: 9340,
				},
			},
		},
	}, {
		desc: "lemming: defaults not overriding",
		ni: &node.Impl{
			Proto: &tpb.Node{
				Name:  "test_node",
				Model: modelLemming,
				Config: &tpb.Config{
					Image:        "lemming:test",
					InitImage:    "foo:latest",
					Command:      []string{"/lemming/lemming2"},
					Args:         []string{"-v=2"},
					EntryCommand: "kubectl exec -it test_node -- /bin/sh",
					Cert:         &tpb.CertificateCfg{},
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
			Name:  "test_node",
			Model: modelLemming,
			Config: &tpb.Config{
				Image:        "lemming:test",
				InitImage:    "foo:latest",
				Command:      []string{"/lemming/lemming2"},
				Args:         []string{"-v=2"},
				EntryCommand: "kubectl exec -it test_node -- /bin/sh",
				Cert:         &tpb.CertificateCfg{},
			},
			Constraints: map[string]string{
				"cpu":    "10",
				"memory": "1Gi",
			},
			Labels: map[string]string{
				"custom":       "value",
				"ondatra-role": "DUT",
				"vendor":       "OPENCONFIG",
			},
			Services: map[uint32]*tpb.Service{
				8080: {
					Name:   "gnmi",
					Inside: 8080,
				},
			},
		},
	}, {
		desc: "magna: empty pb",
		ni: &node.Impl{
			Proto: &tpb.Node{
				Model: modelMagna,
			},
		},
		wantPB: &tpb.Node{
			Model: modelMagna,
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
			Labels: map[string]string{
				"ondatra-role": "ATE",
				"vendor":       "OPENCONFIG",
			},
			Services: map[uint32]*tpb.Service{
				40051: {
					Names:  []string{"grpc"},
					Inside: 40051,
				},
				50051: {
					Names:  []string{"gnmi"},
					Inside: 50051,
				},
			},
		},
	}, {
		desc: "magna: provided service",
		ni: &node.Impl{
			Proto: &tpb.Node{
				Model: modelMagna,
				Config: &tpb.Config{
					Command: []string{"do", "run"},
				},
				Services: map[uint32]*tpb.Service{
					22: {
						Names:  []string{"ssh"},
						Inside: 22,
					},
				},
			},
		},
		wantPB: &tpb.Node{
			Model: modelMagna,
			Config: &tpb.Config{
				Command:      []string{"do", "run"},
				EntryCommand: fmt.Sprintf("kubectl exec -it %s -- sh", ""),
				Image:        "magna:latest",
			},
			Labels: map[string]string{
				"ondatra-role": "ATE",
				"vendor":       "OPENCONFIG",
			},
			Services: map[uint32]*tpb.Service{
				22: {
					Names:  []string{"ssh"},
					Inside: 22,
				},
				40051: {
					Names:  []string{"grpc"},
					Inside: 40051,
				},
				50051: {
					Names:  []string{"gnmi"},
					Inside: 50051,
				},
			},
		},
	}, {
		desc: "magna: provided config command",
		ni: &node.Impl{
			Proto: &tpb.Node{
				Model: modelMagna,
				Config: &tpb.Config{
					Command: []string{"do", "run"},
				},
			},
		},
		wantPB: &tpb.Node{
			Model: modelMagna,
			Config: &tpb.Config{
				Command:      []string{"do", "run"},
				EntryCommand: fmt.Sprintf("kubectl exec -it %s -- sh", ""),
				Image:        "magna:latest",
			},
			Labels: map[string]string{
				"ondatra-role": "ATE",
				"vendor":       "OPENCONFIG",
			},
			Services: map[uint32]*tpb.Service{
				40051: {
					Names:  []string{"grpc"},
					Inside: 40051,
				},
				50051: {
					Names:  []string{"gnmi"},
					Inside: 50051,
				},
			},
		},
	}, {
		desc: "magna: service already defined",
		ni: &node.Impl{
			Proto: &tpb.Node{
				Model: modelMagna,
				Config: &tpb.Config{
					Command: []string{"do", "run"},
				},
				Services: map[uint32]*tpb.Service{
					40051: {
						Names:  []string{"foobar"},
						Inside: 40051,
					},
				},
			},
		},
		wantPB: &tpb.Node{
			Model: modelMagna,
			Config: &tpb.Config{
				Command:      []string{"do", "run"},
				EntryCommand: fmt.Sprintf("kubectl exec -it %s -- sh", ""),
				Image:        "magna:latest",
			},
			Labels: map[string]string{
				"ondatra-role": "ATE",
				"vendor":       "OPENCONFIG",
			},
			Services: map[uint32]*tpb.Service{
				40051: {
					Names:  []string{"foobar"},
					Inside: 40051,
				},
				50051: {
					Names:  []string{"gnmi"},
					Inside: 50051,
				},
			},
		},
	}, {
		desc: "labels already specified",
		ni: &node.Impl{
			Proto: &tpb.Node{
				Model: modelMagna,
				Config: &tpb.Config{
					Command: []string{"do", "run"},
				},
				Labels: map[string]string{
					"foo": "bar",
				},
			},
		},
		wantPB: &tpb.Node{
			Model: modelMagna,
			Config: &tpb.Config{
				Command:      []string{"do", "run"},
				EntryCommand: fmt.Sprintf("kubectl exec -it %s -- sh", ""),
				Image:        "magna:latest",
			},
			Services: map[uint32]*tpb.Service{
				40051: {
					Names:  []string{"grpc"},
					Inside: 40051,
				},
				50051: {
					Names:  []string{"gnmi"},
					Inside: 50051,
				},
			},
			Labels: map[string]string{
				"ondatra-role": "ATE",
				"foo":          "bar",
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
			if d := cmp.Diff(impl.GetProto(), tt.wantPB, protocmp.Transform()); d != "" {
				t.Fatalf("New() failed: diff %s", d)
			}
		})
	}
}

func TestDefaultNodeConstraints(t *testing.T) {
	n := &Node{
		Impl: &node.Impl{
			Proto: &tpb.Node{Model: modelLemming},
		},
	}
	constraints := n.DefaultNodeConstraints()
	if constraints.CPU != defaultLemmingCPU {
		t.Errorf("DefaultNodeConstraints() returned unexpected CPU: got %s, want %s", constraints.CPU, defaultLemmingCPU)
	}

	if constraints.Memory != defaultLemmingMem {
		t.Errorf("DefaultNodeConstraints() returned unexpected Memory: got %s, want %s", constraints.Memory, defaultLemmingMem)
	}
}
