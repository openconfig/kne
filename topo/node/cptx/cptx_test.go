// Juniper cPTX for KNE
// Copyright (c) Juniper Networks, Inc., 2021. All rights reserved.

package cptx

import (
	"context"
	"fmt"
	"os"
	"testing"
	"time"

	"github.com/google/go-cmp/cmp"
	tpb "github.com/google/kne/proto/topo"
	"github.com/google/kne/topo/node"
	"github.com/h-fam/errdiff"
	scraplibase "github.com/scrapli/scrapligo/driver/base"
	scraplicore "github.com/scrapli/scrapligo/driver/core"
	scraplinetwork "github.com/scrapli/scrapligo/driver/network"
	scraplitest "github.com/scrapli/scrapligo/util/testhelper"
	"google.golang.org/protobuf/testing/protocmp"
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

	validPb := &tpb.Node{
		Name:   "pod1",
		Type:   2,
		Config: &tpb.Config{},
	}

	tests := []struct {
		desc     string
		wantErr  bool
		ni       *node.Impl
		testFile string
		testConf string
	}{
		{
			// successfully push config
			desc:    "success",
			wantErr: false,
			ni: &node.Impl{
				KubeClient: ki,
				Namespace:  "test",
				Proto:      validPb,
			},
			testFile: "config_push_success",
			testConf: "cptx-config",
		},
		{
			// We encounter unqxpected response -- we expect to fail
			desc:    "failure",
			wantErr: true,
			ni: &node.Impl{
				KubeClient: ki,
				Namespace:  "test",
				Proto:      validPb,
			},
			testFile: "config_push_failure",
			testConf: "cptx-config",
		},
	}

	for _, tt := range tests {
		t.Run(tt.desc, func(t *testing.T) {
			nImpl, err := New(tt.ni)
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

			err = n.ConfigPush(ctx, fp)
			if err != nil && !tt.wantErr {
				t.Fatalf("config push test failed, error: %+v\n", err)
			}
		})
	}
}

// Test custom cptx
func TestNew(t *testing.T) {
	tests := []struct {
		desc    string
		ni      *node.Impl
		want    *tpb.Node
		wantErr string
		cErr    string
	}{{
		desc:    "nil node impl",
		wantErr: "nodeImpl cannot be nil",
	}, {
		desc: "empty proto",
		ni: &node.Impl{
			KubeClient: fake.NewSimpleClientset(),
			Namespace:  "test",
			Proto: &tpb.Node{
				Name: "pod1",
			},
		},
		want: defaults(&tpb.Node{
			Name: "pod1",
		}),
	}, {
		desc:    "nil pb",
		ni:      &node.Impl{},
		wantErr: "nodeImpl.Proto cannot be nil",
	}, {
		desc: "full proto",
		ni: &node.Impl{
			KubeClient: fake.NewSimpleClientset(),
			Namespace:  "test",
			Proto: &tpb.Node{
				Name: "pod1",
				Config: &tpb.Config{
					ConfigFile: "foo",
					ConfigPath: "/",
					ConfigData: &tpb.Config_Data{
						Data: []byte("config file data"),
					},
				},
				Labels: map[string]string{
					"type": "foo_test",
				},
			},
		},
		want: &tpb.Node{
			Name: "pod1",
			Constraints: map[string]string{
				"cpu":    "8",
				"memory": "8Gi",
			},
			Services: map[uint32]*tpb.Service{
				443: {
					Name:     "ssl",
					Inside:   443,
				},
				22: {
					Name:     "ssh",
					Inside:   22,
				},
				50051: {
					Name:     "gnmi",
					Inside:   50051,
				},
			},
			Labels: map[string]string{
				"type":   "foo_test",
				"vendor": tpb.Vendor_JUNIPER.String(),
			},
			Config: &tpb.Config{
				Image: "cptx:latest",
				Command: []string{
					"/entrypoint.sh",
				},
				Env: map[string]string{
					"CPTX": "1",
				},
				EntryCommand: fmt.Sprintf("kubectl exec -it pod1 -- cli -c"),
				ConfigPath:   "/",
				ConfigFile:   "foo",
				ConfigData: &tpb.Config_Data{
					Data: []byte("config file data"),
				},
			},
		},
	}, {
		desc: "defaults check with empty proto",
		ni: &node.Impl{
			KubeClient: fake.NewSimpleClientset(),
			Namespace:  "test",
			Proto:      &tpb.Node{},
		},
		want: &tpb.Node{
			Constraints: map[string]string{
				"cpu":    "8",
				"memory": "8Gi",
			},
			Services: map[uint32]*tpb.Service{
				443: {
					Name:     "ssl",
					Inside:   443,
				},
				22: {
					Name:     "ssh",
					Inside:   22,
				},
				50051: {
					Name:     "gnmi",
					Inside:   50051,
				},
			},
			Labels: map[string]string{
				"type":   tpb.Node_JUNIPER_CEVO.String(),
				"vendor": tpb.Vendor_JUNIPER.String(),
			},
			Config: &tpb.Config{
				Image: "cptx:latest",
				Command: []string{
					"/entrypoint.sh",
				},
				Env: map[string]string{
					"CPTX": "1",
				},
				EntryCommand: fmt.Sprintf("kubectl exec -it  -- cli -c"),
				ConfigPath:   "/home/evo/configdisk",
				ConfigFile:   "juniper.conf",
			},
		},
	}}
	for _, tt := range tests {
		t.Run(tt.desc, func(t *testing.T) {
			n, err := New(tt.ni)
			if s := errdiff.Check(err, tt.wantErr); s != "" {
				t.Fatalf("Unexpected error: %s", s)
			}
			if err != nil {
				return
			}
			if s := cmp.Diff(n.GetProto(), tt.want, protocmp.Transform(), protocmp.IgnoreFields(&tpb.Service{}, "node_port")); s != "" {
				t.Fatalf("Protos not equal: %s", s)
			}
			err = n.Create(context.Background())
			if s := errdiff.Check(err, tt.cErr); s != "" {
				t.Fatalf("Unexpected error: %s", s)
			}
		})
	}
}
