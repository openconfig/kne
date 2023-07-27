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
package nokia

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/h-fam/errdiff"
	topopb "github.com/openconfig/kne/proto/topo"
	"github.com/openconfig/kne/topo/node"
	scrapliopts "github.com/scrapli/scrapligo/driver/options"
	scraplilogging "github.com/scrapli/scrapligo/logging"
	scraplitransport "github.com/scrapli/scrapligo/transport"
	scrapliutil "github.com/scrapli/scrapligo/util"
	"google.golang.org/protobuf/encoding/prototext"
	"google.golang.org/protobuf/proto"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/watch"
	"k8s.io/client-go/kubernetes/fake"
	"k8s.io/client-go/rest"
	ktest "k8s.io/client-go/testing"
	ctrlclient "sigs.k8s.io/controller-runtime/pkg/client"
)

type fakeWatch struct {
	e []watch.Event
}

// scrapliDebug checks if SCRAPLI_DEBUG env var is set.
// used in testing to enable debug log of scrapligo.
func scrapliDebug() bool {
	_, set := os.LookupEnv("SCRAPLI_DEBUG")

	return set
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

// patchSrlinuxClient patches newSrlinuxClient function to use a fake no-op client for tests.
// Returns a function to restore the original newSrlinuxClient function.
func patchSrlinuxClient() func() {
	origNewSrlinuxClient := newSrlinuxClient

	// overwrite the controller-runtime new function with a fake no-op client as it is not used in tests.
	newSrlinuxClient = func(_ *rest.Config) (ctrlclient.Client, error) {
		return nil, nil
	}

	return func() {
		newSrlinuxClient = origNewSrlinuxClient
	}
}

func TestNew(t *testing.T) {
	unpatchClient := patchSrlinuxClient()
	defer unpatchClient()

	tests := []struct {
		desc    string
		nImpl   *node.Impl
		want    *topopb.Node
		wantErr string
	}{{
		desc:    "nil impl",
		wantErr: "nodeImpl cannot be nil",
	}, {
		desc:    "nil pb",
		wantErr: "nodeImpl.Proto cannot be nil",
		nImpl:   &node.Impl{},
	}, {
		desc: "empty pb defaults",
		nImpl: &node.Impl{
			Proto: &topopb.Node{
				Labels: map[string]string{
					"foo": "test_label",
				},
			},
		},
		want: &topopb.Node{
			Config: &topopb.Config{
				Image:      "ghcr.io/nokia/srlinux:latest",
				ConfigFile: "config.cli",
			},
			Labels: map[string]string{
				"vendor":       "NOKIA",
				"foo":          "test_label",
				"ondatra-role": "DUT",
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
				9337: {
					Name:   "gnoi",
					Inside: 57400,
				},
				9339: {
					Name:   "gnmi",
					Inside: 57400,
				},
				9340: {
					Name:   "gribi",
					Inside: 57401,
				},
				9559: {
					Name:   "p4rt",
					Inside: 9559,
				},
			},
			Constraints: map[string]string{
				"cpu":    "0.5",
				"memory": "1Gi",
			},
		},
	}, {
		desc: "json config file",
		nImpl: &node.Impl{
			Proto: &topopb.Node{
				Labels: map[string]string{
					"foo": "test_label",
				},
				Config: &topopb.Config{
					ConfigFile: "config.json",
				},
			},
		},
		want: &topopb.Node{
			Config: &topopb.Config{
				Image:      "ghcr.io/nokia/srlinux:latest",
				ConfigFile: "config.json",
			},
			Labels: map[string]string{
				"vendor":       "NOKIA",
				"foo":          "test_label",
				"ondatra-role": "DUT",
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
				9337: {
					Name:   "gnoi",
					Inside: 57400,
				},
				9339: {
					Name:   "gnmi",
					Inside: 57400,
				},
				9340: {
					Name:   "gribi",
					Inside: 57401,
				},
				9559: {
					Name:   "p4rt",
					Inside: 9559,
				},
			},
			Constraints: map[string]string{
				"cpu":    "0.5",
				"memory": "1Gi",
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
				t.Fatalf("New() failed: got\n%swant\n%s", prototext.Format(n.GetProto()), prototext.Format(tt.want))
			}
		})
	}
}
func TestGenerateSelfSigned(t *testing.T) {
	unpatchClient := patchSrlinuxClient()
	defer unpatchClient()

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
			Vendor: topopb.Vendor_NOKIA,
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
			testFile: "testdata/generate_certificate_success",
		},
		{
			// device returns "Error: something bad happened" -- we expect to fail
			desc:     "failure",
			wantErr:  true,
			ni:       ni,
			testFile: "testdata/generate_certificate_failure",
		},
	}

	for _, tt := range tests {
		t.Run(tt.desc, func(t *testing.T) {
			nImpl, err := New(tt.ni)

			if err != nil {
				t.Fatalf("failed creating kne srlinux node")
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

			if scrapliDebug() {
				li, err := scraplilogging.NewInstance(
					scraplilogging.WithLevel("debug"),
					scraplilogging.WithLogger(t.Log))
				if err != nil {
					t.Fatalf("failed created scrapligo logger %v\n", err)
				}

				n.testOpts = append(n.testOpts, scrapliopts.WithLogger(li))
			}

			ctx := context.Background()

			err = n.GenerateSelfSigned(ctx)
			if err != nil && !tt.wantErr {
				t.Fatalf("generating self signed cert failed, error: %+v\n", err)
			}
		})
	}
}

func TestResetCfg(t *testing.T) {
	unpatchClient := patchSrlinuxClient()
	defer unpatchClient()

	ki := fake.NewSimpleClientset(&corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name: "pod1",
		},
	})

	ni := &node.Impl{
		KubeClient: ki,
		Namespace:  "test",
		Proto: &topopb.Node{
			Name:   "pod1",
			Vendor: topopb.Vendor_NOKIA,
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
			testFile: "testdata/reset_config_success",
		},
		{
			// device returns "Error: %s" -- we expect to fail
			desc:     "failure",
			wantErr:  true,
			ni:       ni,
			testFile: "testdata/reset_config_failure",
		},
	}

	for _, tt := range tests {
		t.Run(tt.desc, func(t *testing.T) {
			nImpl, err := New(tt.ni)

			if err != nil {
				t.Fatalf("failed creating srlinux node")
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

			if scrapliDebug() {
				li, err := scraplilogging.NewInstance(
					scraplilogging.WithLevel("debug"),
					scraplilogging.WithLogger(t.Log))
				if err != nil {
					t.Fatalf("failed created scrapligo logger %v\n", err)
				}

				n.testOpts = append(n.testOpts, scrapliopts.WithLogger(li))
			}

			ctx := context.Background()

			err = n.ResetCfg(ctx)
			if err != nil && !tt.wantErr {
				t.Fatalf("resetting config failed, error: %+v\n", err)
			}
		})
	}
}

func TestConfigPush(t *testing.T) {
	unpatchClient := patchSrlinuxClient()
	defer unpatchClient()

	ki := fake.NewSimpleClientset(&corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name: "pod1",
		},
	})

	ni := &node.Impl{
		KubeClient: ki,
		Namespace:  "test",
		Proto: &topopb.Node{
			Name:   "pod1",
			Vendor: topopb.Vendor_NOKIA,
			Config: &topopb.Config{},
		},
	}

	tests := []struct {
		desc     string
		wantErr  bool
		ni       *node.Impl
		testFile string
		cmdFile  string
	}{
		{
			// successfully configure certificate
			desc:     "success",
			wantErr:  false,
			ni:       ni,
			testFile: "testdata/configpush_success",
			cmdFile:  "testdata/configpush_success_cli.cfg",
		},
		{
			// device returns an error during config push -- we expect to fail
			desc:     "failure",
			wantErr:  true,
			ni:       ni,
			testFile: "testdata/configpush_failure",
			cmdFile:  "testdata/configpush_failure_cli.cfg",
		},
	}

	for _, tt := range tests {
		t.Run(tt.desc, func(t *testing.T) {
			nImpl, err := New(tt.ni)

			if err != nil {
				t.Fatalf("failed creating srlinux node")
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

			if scrapliDebug() {
				li, err := scraplilogging.NewInstance(
					scraplilogging.WithLevel("debug"),
					scraplilogging.WithLogger(t.Log))
				if err != nil {
					t.Fatalf("failed created scrapligo logger %v\n", err)
				}

				n.testOpts = append(n.testOpts, scrapliopts.WithLogger(li))
			}

			ctx := context.Background()

			r, err := os.Open(tt.cmdFile)
			if err != nil {
				t.Fatal(err)
			}

			err = n.ConfigPush(ctx, r)
			if err != nil && !tt.wantErr {
				t.Fatalf("config push failed, error: %+v\n", err)
			}
		})
	}
}
