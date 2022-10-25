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
package ceos

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/h-fam/errdiff"
	topopb "github.com/openconfig/kne/proto/topo"
	"github.com/openconfig/kne/topo/node"
	scrapliopts "github.com/scrapli/scrapligo/driver/options"
	scraplitransport "github.com/scrapli/scrapligo/transport"
	scrapliutil "github.com/scrapli/scrapligo/util"
	"google.golang.org/protobuf/encoding/prototext"
	"google.golang.org/protobuf/proto"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/watch"
	"k8s.io/client-go/kubernetes/fake"
	ktest "k8s.io/client-go/testing"
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

func TestNew(t *testing.T) {
	tests := []struct {
		desc    string
		nImpl   *node.Impl
		want    *topopb.Node
		wantErr string
	}{
		{
			desc:    "nil nodeImpl",
			wantErr: "nodeImpl cannot be nil",
		}, {
			desc:    "nil pb",
			nImpl:   &node.Impl{},
			wantErr: "nodeImpl.Proto cannot be nil",
		}, {
			desc: "invalid eth intfs 1",
			nImpl: &node.Impl{
				Proto: &topopb.Node{
					Interfaces: map[string]*topopb.Interface{
						"eth1": {Name: "Ethernet1"},
						"eth2": {Name: "Ethernet1/2"},
						"eth3": {Name: "Ethernet1/2/3"},
						"eth4": {Name: "Ethernet1/2/3/4"},
					},
				},
			},
			wantErr: "Unrecognized interface name: Ethernet1/2/3/4",
		}, {
			desc: "invalid eth intfs 2",
			nImpl: &node.Impl{
				Proto: &topopb.Node{
					Interfaces: map[string]*topopb.Interface{
						"eth1": {Name: "Ethernet"},
					},
				},
			},
			wantErr: "Unrecognized interface name: Ethernet",
		}, {
			desc: "invalid management intfs 1",
			nImpl: &node.Impl{
				Proto: &topopb.Node{
					Interfaces: map[string]*topopb.Interface{
						"eth1": {Name: "Management1"},
						"eth2": {Name: "Management1/2"},
						"eth3": {Name: "Management1/2/3"},
					},
				},
			},
			wantErr: "Unrecognized interface name: Management1/2/3",
		}, {
			desc: "invalid management intfs 2",
			nImpl: &node.Impl{
				Proto: &topopb.Node{
					Interfaces: map[string]*topopb.Interface{
						"eth1": {Name: "Management"},
					},
				},
			},
			wantErr: "Unrecognized interface name: Management",
		}, {
			desc: "default check with empty topo proto",
			nImpl: &node.Impl{
				Proto: &topopb.Node{},
			},
			want: &topopb.Node{
				Config: &topopb.Config{
					EntryCommand: fmt.Sprintf("kubectl exec -it %s -- Cli", ""),
					ConfigPath:   "/mnt/flash",
					ConfigFile:   "startup-config",
				},
				Labels: map[string]string{
					"vendor":  "ARISTA",
					"model":   "",
					"os":      "",
					"version": "",
				},
				Constraints: map[string]string{
					"cpu":    "0.5",
					"memory": "1Gi",
				},
				Services: map[uint32]*topopb.Service{
					443: {
						Name:   "ssl",
						Inside: 443,
					},
					22: {
						Name:   "ssh",
						Inside: 22,
					},
					6030: {
						Name:   "gnmi",
						Inside: 6030,
					},
				},
			},
		}, {
			desc: "with config",
			nImpl: &node.Impl{
				Proto: &topopb.Node{
					Config: &topopb.Config{
						Image: "foo",
						Env: map[string]string{
							"CEOS":      "123",
							"container": "test",
						},
						Args: []string{"biz", "baz"},
						Cert: &topopb.CertificateCfg{
							Config: &topopb.CertificateCfg_SelfSigned{
								SelfSigned: &topopb.SelfSignedCertCfg{
									CertName:   "gnmiCert",
									KeyName:    "gnmiKey",
									KeySize:    4096,
									CommonName: "foo",
								},
							},
						},
					},
					Labels: map[string]string{
						"model": "foo",
						"os":    "bar",
					},
				},
			},
			want: &topopb.Node{
				Config: &topopb.Config{
					EntryCommand: fmt.Sprintf("kubectl exec -it %s -- Cli", ""),
					Image:        "foo",
					ConfigPath:   "/mnt/flash",
					ConfigFile:   "startup-config",
					Env: map[string]string{
						"CEOS":      "123",
						"container": "test",
					},
					Args: []string{"biz", "baz"},
					Cert: &topopb.CertificateCfg{
						Config: &topopb.CertificateCfg_SelfSigned{
							SelfSigned: &topopb.SelfSignedCertCfg{
								CertName:   "gnmiCert",
								KeyName:    "gnmiKey",
								KeySize:    4096,
								CommonName: "foo",
							},
						},
					},
				},
				Labels: map[string]string{
					"vendor":  "ARISTA",
					"model":   "foo",
					"os":      "bar",
					"version": "",
				},
				Constraints: map[string]string{
					"cpu":    "0.5",
					"memory": "1Gi",
				},
				Services: map[uint32]*topopb.Service{
					443: {
						Name:   "ssl",
						Inside: 443,
					},
					22: {
						Name:   "ssh",
						Inside: 22,
					},
					6030: {
						Name:   "gnmi",
						Inside: 6030,
					},
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
				t.Fatalf("New() failed: got\n\n%swant\n\n%s", prototext.Format(n.GetProto()), prototext.Format(tt.want))
			}
		})
	}
}

func TestResetCfg(t *testing.T) {
	ki := fake.NewSimpleClientset(&corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name: "pod1",
		},
	})

	reaction := func(action ktest.Action) (handled bool, ret watch.Interface, err error) {
		f := &fakeWatch{
			e: []watch.Event{{
				Object: &corev1.Pod{
					Status: corev1.PodStatus{
						Phase: corev1.PodRunning,
					},
				},
			}},
		}
		return true, f, nil
	}
	ki.PrependWatchReactor("*", reaction)

	ni := &node.Impl{
		KubeClient: ki,
		Namespace:  "test",
		Proto: &topopb.Node{
			Name:   "pod1",
			Vendor: topopb.Vendor_ARISTA,
			Config: &topopb.Config{},
		},
	}

	tests := []struct {
		desc     string
		wantErr  bool
		ni       *node.Impl
		testFile string
	}{
		{
			// successfully configure certificate
			desc:     "success",
			wantErr:  false,
			ni:       ni,
			testFile: "reset_config_success",
		},
		{
			// device returns "% Invalid input" -- we expect to fail
			desc:     "failure",
			wantErr:  true,
			ni:       ni,
			testFile: "reset_config_failure",
		},
	}

	for _, tt := range tests {
		t.Run(tt.desc, func(t *testing.T) {
			nImpl, err := New(tt.ni)

			if err != nil {
				t.Fatalf("failed creating kne arista node")
			}

			n, _ := nImpl.(*Node)

			n.testOpts = []scrapliutil.Option{
				scrapliopts.WithTransportType(scraplitransport.FileTransport),
				scrapliopts.WithFileTransportFile(tt.testFile),
				scrapliopts.WithTimeoutOps(2 * time.Second),
				scrapliopts.WithTransportReadSize(1),
				scrapliopts.WithReadDelay(0),
				scrapliopts.WithDefaultLogger(),
			}

			ctx := context.Background()

			err = n.ResetCfg(ctx)
			if err != nil && !tt.wantErr {
				t.Fatalf("resetting config failed, error: %+v\n", err)
			}
		})
	}
}
