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
package ceos

import (
	"context"
	"fmt"
	"regexp"
	"testing"
	"time"

	expect "github.com/google/goexpect"
	topopb "github.com/google/kne/proto/topo"
	"github.com/google/kne/topo/node"
	"github.com/h-fam/errdiff"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/watch"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/kubernetes/fake"
	"k8s.io/client-go/rest"
	ktest "k8s.io/client-go/testing"
)

type sends struct {
	req string
	err error
}

type expects struct {
	resp      string
	respSlice []string
	err       error
}

type fakeExpect struct {
	spawnErr error
	expects  []expects
	sends    []sends
	closeErr error
}

func fakeSpawner(f *fakeExpect) func(string, time.Duration, ...expect.Option) (expect.Expecter, <-chan error, error) {
	return func(string, time.Duration, ...expect.Option) (expect.Expecter, <-chan error, error) {
		if f.spawnErr != nil {
			return nil, nil, f.spawnErr
		}
		return f, nil, nil
	}
}

func (f *fakeExpect) Expect(*regexp.Regexp, time.Duration) (string, []string, error) {
	if len(f.expects) == 0 {
		return "", nil, fmt.Errorf("out of expects")
	}
	resp := f.expects[0]
	f.expects = f.expects[1:]
	return resp.resp, resp.respSlice, resp.err
}

func (f *fakeExpect) ExpectBatch([]expect.Batcher, time.Duration) ([]expect.BatchRes, error) {
	return nil, fmt.Errorf("Unimplemented")
}

func (f *fakeExpect) ExpectSwitchCase([]expect.Caser, time.Duration) (string, []string, int, error) {
	return "", nil, 0, fmt.Errorf("Unimplemented")
}

func (f *fakeExpect) Send(string) error {
	if len(f.sends) == 0 {
		return fmt.Errorf("out of sends")
	}
	resp := f.sends[0]
	f.sends = f.sends[1:]
	return resp.err
}

func (f *fakeExpect) Close() error {
	return f.closeErr
}

type fakeNode struct {
	kClient    kubernetes.Interface
	namespace  string
	interfaces map[string]*node.Link
	rCfg       *rest.Config
}

func (f *fakeNode) KubeClient() kubernetes.Interface {
	return f.kClient
}

func (f *fakeNode) RESTConfig() *rest.Config {
	return f.rCfg
}

func (f *fakeNode) Interfaces() map[string]*node.Link {
	return f.interfaces
}

func (f *fakeNode) Namespace() string {
	return f.namespace
}

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

var (
	validPb = &topopb.Node{
		Name: "pod1",
		Config: &topopb.Config{
			Cert: &topopb.CertificateCfg{
				Config: &topopb.CertificateCfg_SelfSigned{
					SelfSigned: &topopb.SelfSignedCertCfg{
						CertName:   "testCert.pem",
						KeyName:    "testCertKey.pem",
						KeySize:    4096,
						CommonName: "r1",
					},
				},
			},
		},
	}
)

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
	tests := []struct {
		desc    string
		wantErr string
		ni      node.Interface
		pb      *topopb.Node
		spawner func(string, time.Duration, ...expect.Option) (expect.Expecter, <-chan error, error)
	}{{
		desc: "no pb",
	}, {
		desc: "valid pb",
		pb:   validPb,
		ni: &fakeNode{
			kClient:   ki,
			namespace: "test",
		},
		spawner: fakeSpawner(&fakeExpect{
			expects: []expects{
				{resp: "some stuff>"}, // base
				{resp: "prompt#"},     // enable
				{resp: "prompt#"},     // key gen
				{resp: "prompt#"},     // cert gen
			},
			sends: []sends{
				{err: nil}, // enable
				{err: nil}, // key gen
				{err: nil}, // cert gen
			},
		}),
	}, {
		desc: "retry first prompt",
		pb:   validPb,
		ni: &fakeNode{
			kClient:   ki,
			namespace: "test",
		},
		spawner: fakeSpawner(&fakeExpect{
			expects: []expects{
				{err: fmt.Errorf("promptErr")},
				{resp: "prompt>"}, // base
				{resp: "prompt#"}, // enable
				{resp: "prompt#"}, // key gen
				{resp: "prompt#"}, // cert gen
			},
			sends: []sends{
				{err: nil}, // enable
				{err: nil}, // key gen
				{err: nil}, // cert gen
			},
		}),
	}, {
		desc: "retry enable",
		pb:   validPb,
		ni: &fakeNode{
			kClient:   ki,
			namespace: "test",
		},
		spawner: fakeSpawner(&fakeExpect{
			expects: []expects{
				{resp: "some stuff>"},          // base
				{err: fmt.Errorf("enableErr")}, // enable
				{resp: "prompt>"},              // base
				{resp: "prompt#"},              // enable
				{resp: "prompt#"},              // key gen
				{resp: "prompt#"},              // cert gen
			},
			sends: []sends{
				{err: nil}, // enable (fail)
				{err: nil}, // enable
				{err: nil}, // key gen
				{err: nil}, // cert gen
			},
		}),
	}, {
		desc: "close err",
		pb:   validPb,
		ni: &fakeNode{
			kClient:   ki,
			namespace: "test",
		},
		spawner: fakeSpawner(&fakeExpect{
			expects: []expects{
				{resp: "prompt>"}, // base
				{resp: "prompt#"}, // enable
				{resp: "prompt#"}, // key gen
				{resp: "prompt#"}, // cert gen
			},
			sends: []sends{
				{err: nil}, // enable
				{err: nil}, // key gen
				{err: nil}, // cert gen
			},
			closeErr: fmt.Errorf("close err"),
		}),
		wantErr: "close err",
	}}
	for _, tt := range tests {
		t.Run(tt.desc, func(t *testing.T) {
			spawner = tt.spawner
			timeSecond = 0
			defer func() {
				spawner = defaultSpawner
				timeSecond = time.Second
			}()
			nImpl, _ := New(tt.pb)
			n := nImpl.(*Node)
			ctx := context.Background()
			err := n.GenerateSelfSigned(ctx, tt.ni)
			if s := errdiff.Check(err, tt.wantErr); s != "" {
				t.Fatalf("GenerateSelfSigned unexpected error: %s", s)
			}
			if tt.wantErr != "" {
				return
			}
		})
	}
}
