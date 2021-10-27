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
package srl

import (
	"context"
	"log"
	"testing"
	"time"

	topopb "github.com/google/kne/proto/topo"
	"github.com/google/kne/topo/node"
	scraplibase "github.com/scrapli/scrapligo/driver/base"
	scraplinetwork "github.com/scrapli/scrapligo/driver/network"
	"github.com/scrapli/scrapligo/logging"
	scraplitest "github.com/scrapli/scrapligo/util/testhelper"
	srlinux "github.com/srl-labs/srlinux-scrapli"
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

func TestGenerateSelfSigned(t *testing.T) {
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
			Name: "pod1",
			Type: 2,
			Config: &topopb.Config{
				Cert: &topopb.CertificateCfg{
					Config: &topopb.CertificateCfg_SelfSigned{
						SelfSigned: &topopb.SelfSignedCertCfg{
							CertName: "test",
							KeyName:  "my_key",
							KeySize:  2048,
						},
					},
				},
			},
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
			testFile: "generate_certificate_success",
		},
		{
			// device returns "Error: something bad happened" -- we expect to fail
			desc:     "failure",
			wantErr:  true,
			ni:       ni,
			testFile: "generate_certificate_failure",
		},
	}
	logging.SetDebugLogger(log.Print)
	for _, tt := range tests {
		t.Run(tt.desc, func(t *testing.T) {
			nImpl, err := New(tt.ni)

			if err != nil {
				t.Fatalf("failed creating kne srlinux node")
			}

			n, _ := nImpl.(*Node)

			oldSRLinuxDriver := srlinux.NewSRLinuxDriver
			defer func() { srlinux.NewSRLinuxDriver = oldSRLinuxDriver }()
			srlinux.NewSRLinuxDriver = func(host string, options ...scraplibase.Option) (*scraplinetwork.Driver, error) {
				return srlinux.NewPatchedSRLinuxDriver(
					host,
					scraplibase.WithAuthBypass(true),
					scraplibase.WithTimeoutOps(2*time.Second),
					scraplitest.WithPatchedTransport(tt.testFile),
				)
			}

			ctx := context.Background()

			err = n.GenerateSelfSigned(ctx)
			if err != nil && !tt.wantErr {
				t.Fatalf("generating self signed cert failed, error: %+v\n", err)
			}
		})
	}
}
