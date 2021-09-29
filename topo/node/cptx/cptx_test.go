// Juniper cPTX for KNE
// Copyright (c) Juniper Networks, Inc., 2021. All rights reserved.

package cptx

import (
	"context"
	"testing"
	"time"
    "os"

	topopb "github.com/google/kne/proto/topo"
	"github.com/google/kne/topo/node"
	scraplibase "github.com/scrapli/scrapligo/driver/base"
	scraplicore "github.com/scrapli/scrapligo/driver/core"
	scraplinetwork "github.com/scrapli/scrapligo/driver/network"
	scraplitest "github.com/scrapli/scrapligo/util/testhelper"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/watch"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/kubernetes/fake"
	"k8s.io/client-go/rest"
	ktest "k8s.io/client-go/testing"
)

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

func TestConfigPush(t *testing.T) {
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

	ni := &fakeNode{
		kClient:   ki,
		namespace: "test",
	}

	validPb := &topopb.Node{
		Name:   "pod1",
		Type:   2,
		Config: &topopb.Config{},
	}

	tests := []struct {
		desc     string
		wantErr  bool
		ni       node.Interface
		pb       *topopb.Node
		testFile string
        testConf string
	}{
		{
			// successfully push config
			desc:     "success",
			wantErr:  false,
			ni:       ni,
			pb:       validPb,
			testFile: "config_push_success",
			testConf: "cptx-config",
		},
		{
			// We encounter unqxpected response -- we expect to fail
			desc:     "failure",
			wantErr:  true,
			ni:       ni,
			pb:       validPb,
			testFile: "config_push_failure",
			testConf: "cptx-config",
		},
	}

	for _, tt := range tests {
		t.Run(tt.desc, func(t *testing.T) {
			nImpl, err := New(tt.pb)

			if err != nil {
				t.Fatalf("failed creating kne juniper node")
			}

			n, _ := nImpl.(*Node)

			oldNewCoreDriver := scraplicore.NewCoreDriver
			defer func() { scraplicore.NewCoreDriver = oldNewCoreDriver }()
			scraplicore.NewCoreDriver = func(host, platform string, options ...scraplibase.Option) (*scraplinetwork.Driver, error) {
				return scraplicore.NewJUNOSDriver(
					host,
					scraplibase.WithAuthBypass(true),
					scraplibase.WithTimeoutOps(1*time.Second),
					scraplitest.WithPatchedTransport(tt.testFile),
				)
			}

            fp, err := os.Open(tt.testConf)
            if err != nil {
				t.Fatalf("unable to open file, error: %+v\n", err)
            }
            defer fp.Close()

			ctx := context.Background()

			err = n.ConfigPush(ctx, ni.Namespace(), fp)
			if err != nil && !tt.wantErr {
				t.Fatalf("config push test failed, error: %+v\n", err)
			}
		})
	}
}
