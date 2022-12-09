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
	"context"
	"fmt"
	"testing"

	"github.com/google/go-cmp/cmp"
	"github.com/h-fam/errdiff"
	"github.com/openconfig/kne/topo/node"
	"github.com/openconfig/lemming/operator/api/clientset"
	"github.com/openconfig/lemming/operator/api/clientset/fake"
	"google.golang.org/protobuf/encoding/prototext"
	"google.golang.org/protobuf/proto"

	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/watch"
	"k8s.io/client-go/rest"

	tpb "github.com/openconfig/kne/proto/topo"
	lemmingv1 "github.com/openconfig/lemming/operator/api/lemming/v1alpha1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	v1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	k8stesting "k8s.io/client-go/testing"
)

type fakeWatch struct {
	e []watch.Event
}

func (f *fakeWatch) Stop() {}

func (f *fakeWatch) ResultChan() <-chan watch.Event {
	eCh := make(chan watch.Event)
	go func() {
		for len(f.e) != 0 {
			e := f.e[0]
			f.e = f.e[1:]
			eCh <- e
		}
	}()
	return eCh
}

func TestCreate(t *testing.T) {
	tests := []struct {
		desc        string
		n           *Node
		clientFnErr error
		createErr   error
		watchErr    error
		wantErr     string
		want        *lemmingv1.Lemming
	}{{
		desc: "error creating client set",
		n: &Node{
			Impl: &node.Impl{
				Proto: &tpb.Node{
					Config: &tpb.Config{
						Command: []string{"/lemming"},
					},
				},
			},
		},
		clientFnErr: fmt.Errorf("client err"),
		wantErr:     "client err",
	}, {
		desc: "create error",
		n: &Node{
			Impl: &node.Impl{
				Proto: &tpb.Node{
					Config: &tpb.Config{
						Command: []string{"/lemming"},
					},
				},
			},
		},
		createErr: fmt.Errorf("create err"),
		wantErr:   "create err",
	}, {
		desc: "watch error",
		n: &Node{
			Impl: &node.Impl{
				Proto: &tpb.Node{
					Config: &tpb.Config{
						Command: []string{"/lemming"},
					},
				},
			},
		},
		watchErr: fmt.Errorf("watch err"),
		wantErr:  "watch err",
	}, {
		desc: "success",
		n: &Node{
			Impl: &node.Impl{
				Namespace: "default",
				Proto: &tpb.Node{
					Name: "test",
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
						6030: {
							Inside:  6030,
							Outside: 6030,
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
				Ports:          map[string]lemmingv1.ServicePort{"gnmi": {InnerPort: 6030, OuterPort: 6030}},
				InterfaceCount: 1,
				Resources: corev1.ResourceRequirements{
					Requests: corev1.ResourceList{},
				},
				TLS: lemmingv1.TLSSpec{
					SelfSigned: lemmingv1.SelfSignedSpec{
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
			cs.PrependWatchReactor("lemmings", func(action k8stesting.Action) (handled bool, ret watch.Interface, err error) {
				return true, &fakeWatch{e: []watch.Event{{
					Object: &lemmingv1.Lemming{
						Status: lemmingv1.LemmingStatus{
							Phase: lemmingv1.Running,
						},
					},
				}}}, tt.watchErr
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
			got, err := cs.LemmingV1alpha1().Lemmings("default").Get(context.Background(), "test", v1.GetOptions{})
			if err != nil {
				t.Fatal(err)
			}
			if d := cmp.Diff(tt.want, got); d != "" {
				t.Errorf("unexpected diff (-want +got):\n%s", d)
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
				Image:        "us-west1-docker.pkg.dev/openconfig-lemming/release/lemming:ga",
				InitImage:    node.DefaultInitContainerImage,
				Command:      []string{"/lemming/lemming"},
				Args:         []string{"--alsologtostderr", "--enable_dataplane"},
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
