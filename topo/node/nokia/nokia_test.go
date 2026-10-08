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

	"github.com/google/go-cmp/cmp"
	"github.com/openconfig/gnmi/errdiff"
	npb "github.com/openconfig/kne/proto/nokia"
	topopb "github.com/openconfig/kne/proto/topo"
	"github.com/openconfig/kne/topo/node"
	scrapliopts "github.com/scrapli/scrapligo/driver/options"
	scraplilogging "github.com/scrapli/scrapligo/logging"
	scraplitransport "github.com/scrapli/scrapligo/transport"
	scrapliutil "github.com/scrapli/scrapligo/util"
	"google.golang.org/protobuf/encoding/prototext"
	"google.golang.org/protobuf/testing/protocmp"
	"google.golang.org/protobuf/types/known/anypb"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/watch"
	"k8s.io/client-go/kubernetes/fake"
	ktest "k8s.io/client-go/testing"
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

func TestNew(t *testing.T) {
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
			Model: "ixrd2",
			Os:    "nokia_srlinux",
			Config: &topopb.Config{
				Image:      "ghcr.io/nokia/srlinux:latest",
				InitImage:  "us-west1-docker.pkg.dev/kne-external/kne/init-wait:ga",
				ConfigFile: "config.cli",
			},
			Labels: map[string]string{
				"vendor":       "NOKIA",
				"foo":          "test_label",
				"ondatra-role": "DUT",
				"model":        "ixrd2",
				"os":           "nokia_srlinux",
			},
			Services: map[uint32]*topopb.Service{
				443: {
					Names:  []string{"ssl"},
					Inside: 443,
				},
				22: {
					Names:  []string{"ssh"},
					Inside: 22,
				},
				9339: {
					Names:  []string{"gnmi", "gnoi", "gnsi"},
					Inside: 57400,
				},
				9340: {
					Names:  []string{"gribi"},
					Inside: 57401,
				},
				9559: {
					Names:  []string{"p4rt"},
					Inside: 9559,
				},
			},
			Constraints: map[string]string{
				"cpu":    "2000m",
				"memory": "4Gi",
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
			Model: "ixrd2",
			Os:    "nokia_srlinux",
			Config: &topopb.Config{
				Image:      "ghcr.io/nokia/srlinux:latest",
				InitImage:  "us-west1-docker.pkg.dev/kne-external/kne/init-wait:ga",
				ConfigFile: "config.json",
			},
			Labels: map[string]string{
				"vendor":       "NOKIA",
				"foo":          "test_label",
				"ondatra-role": "DUT",
				"model":        "ixrd2",
				"os":           "nokia_srlinux",
			},
			Services: map[uint32]*topopb.Service{
				443: {
					Names:  []string{"ssl"},
					Inside: 443,
				},
				22: {
					Names:  []string{"ssh"},
					Inside: 22,
				},
				9339: {
					Names:  []string{"gnmi", "gnoi", "gnsi"},
					Inside: 57400,
				},
				9340: {
					Names:  []string{"gribi"},
					Inside: 57401,
				},
				9559: {
					Names:  []string{"p4rt"},
					Inside: 9559,
				},
			},
			Constraints: map[string]string{
				"cpu":    "2000m",
				"memory": "4Gi",
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
			if diff := cmp.Diff(tt.want, n.GetProto(), protocmp.Transform()); diff != "" {
				t.Fatalf("New() failed: diff (-want, +got): %v\nwant\n\n %s\ngot\n\n%s", diff, prototext.Format(tt.want), prototext.Format(n.GetProto()))
			}
		})
	}
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
						Conditions: []corev1.PodCondition{{
							Type:   corev1.PodReady,
							Status: corev1.ConditionTrue,
						}},
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
				scrapliopts.WithTimeoutOps(10 * time.Second),
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
				scrapliopts.WithTimeoutOps(10 * time.Second),
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
				scrapliopts.WithTimeoutOps(10 * time.Second),
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

func TestDefaultNodeConstraints(t *testing.T) {
	n := &Node{}
	constraints := n.DefaultNodeConstraints()
	if constraints.CPU != defaultConstraints.CPU {
		t.Errorf("DefaultNodeConstraints() returned unexpected CPU: got %s, want %s", constraints.CPU, defaultConstraints.CPU)
	}

	if constraints.Memory != defaultConstraints.Memory {
		t.Errorf("DefaultNodeConstraints() returned unexpected Memory: got %s, want %s", constraints.Memory, defaultConstraints.Memory)
	}
}

func TestEscapeConfig(t *testing.T) {
	tests := []struct {
		desc  string
		input string
		want  string
	}{
		{
			desc:  "plain text",
			input: "set system location foo",
			want:  "set system location foo",
		},
		{
			desc:  "double quotes",
			input: "set / system location \"set with config push\"",
			want:  `set / system location \"set with config push\"`,
		},
		{
			desc:  "dollar signs",
			input: "set / system location $(value)",
			want:  `set / system location \$(value)`,
		},
		{
			desc:  "backticks",
			input: "set / system location `value`",
			want:  "set / system location \\`value\\`",
		},
		{
			desc:  "backslashes and dollar signs",
			input: `set / system location \path\$ENV`,
			want:  `set / system location \\path\\\$ENV`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.desc, func(t *testing.T) {
			got := escapeConfig([]byte(tt.input))
			if got != tt.want {
				t.Errorf("escapeConfig(%q) = %q, want %q", tt.input, got, tt.want)
			}
		})
	}
}

func TestLineCards(t *testing.T) {
	ifaces := func(names ...string) map[string]*topopb.Interface {
		m := map[string]*topopb.Interface{}
		for _, n := range names {
			m[n] = &topopb.Interface{}
		}
		return m
	}
	vendorData := func(t *testing.T, cs ...*npb.Component) *anypb.Any {
		t.Helper()
		a, err := anypb.New(&npb.SRLinuxConfig{Components: cs})
		if err != nil {
			t.Fatal(err)
		}
		return a
	}
	modular := func(names ...string) func(*testing.T) *topopb.Node {
		return func(*testing.T) *topopb.Node {
			return &topopb.Node{Model: "ixr10e", Interfaces: ifaces(names...)}
		}
	}
	slot := func(s int64) map[string]any { return map[string]any{"slot": s} }

	tests := []struct {
		desc    string
		pb      func(t *testing.T) *topopb.Node
		want    []any
		wantErr string
	}{{
		desc: "no interfaces",
		pb:   modular(),
	}, {
		desc: "only slot 1",
		pb:   modular("e1-1", "e1-2", "e1-3-1"),
	}, {
		desc: "non eX-Y names ignored",
		pb:   modular("eth1", "ethernet-2/1", "e2", "mgmt0"),
	}, {
		desc: "slots 1 and 2",
		pb:   modular("e2-1", "e1-1", "e2-5"),
		want: []any{slot(1), slot(2)},
	}, {
		desc: "slot 1 unpopulated",
		pb:   modular("e2-1"),
		want: []any{slot(2)},
	}, {
		desc: "breakout",
		pb:   modular("e3-1-2", "e7-4"),
		want: []any{slot(3), slot(7)},
	}, {
		desc: "slot 0 ignored",
		pb:   modular("e0-1", "e2-1"),
		want: []any{slot(2)},
	}, {
		desc: "slots outside the chassis ignored",
		pb:   modular("e8-1", "e9-1", "e2-1"),
		want: []any{slot(2), slot(8)},
	}, {
		desc: "slots outside a 4 slot chassis ignored",
		pb: func(*testing.T) *topopb.Node {
			return &topopb.Node{Model: "ixr6e", Interfaces: ifaces("e4-1", "e5-1")}
		},
		want: []any{slot(4)},
	}, {
		desc: "slot number overflow ignored",
		pb:   modular("e99999999999999999999-1", "e2-1"),
		want: []any{slot(2)},
	}, {
		desc: "all modular models derive",
		pb: func(*testing.T) *topopb.Node {
			return &topopb.Node{Model: "ixr6", Interfaces: ifaces("e1-1", "e3-1")}
		},
		want: []any{slot(1), slot(3)},
	}, {
		desc: "fixed model does not derive",
		pb: func(*testing.T) *topopb.Node {
			return &topopb.Node{Model: "ixrd3", Interfaces: ifaces("e1-1", "e2-1")}
		},
	}, {
		desc: "no model does not derive",
		pb: func(*testing.T) *topopb.Node {
			return &topopb.Node{Interfaces: ifaces("e1-1", "e2-1")}
		},
	}, {
		desc: "explicit components override derivation",
		pb: func(t *testing.T) *topopb.Node {
			return &topopb.Node{
				Model:      "ixr10e",
				Interfaces: ifaces("e1-1", "e2-1", "e3-1"),
				Config: &topopb.Config{VendorData: vendorData(t,
					&npb.Component{Slot: 2, Type: "imm60-100g-qsfp28"},
					&npb.Component{Slot: 1},
				)},
			}
		},
		want: []any{map[string]any{"slot": int64(2), "type": "imm60-100g-qsfp28"}, slot(1)},
	}, {
		desc: "explicit components are passed through on any model",
		pb: func(t *testing.T) *topopb.Node {
			return &topopb.Node{
				Model:  "ixrd3",
				Config: &topopb.Config{VendorData: vendorData(t, &npb.Component{Slot: 2})},
			}
		},
		want: []any{slot(2)},
	}, {
		desc: "empty vendor data falls back to derivation",
		pb: func(t *testing.T) *topopb.Node {
			return &topopb.Node{
				Model:      "ixr10e",
				Interfaces: ifaces("e1-1", "e2-1"),
				Config:     &topopb.Config{VendorData: vendorData(t)},
			}
		},
		want: []any{slot(1), slot(2)},
	}, {
		desc: "wrong vendor data type",
		pb: func(t *testing.T) *topopb.Node {
			a, err := anypb.New(&topopb.Interface{})
			if err != nil {
				t.Fatal(err)
			}
			return &topopb.Node{Config: &topopb.Config{VendorData: a}}
		},
		wantErr: "failed to unmarshal vendor data",
	}}

	for _, tt := range tests {
		t.Run(tt.desc, func(t *testing.T) {
			got, err := lineCards(tt.pb(t))
			if s := errdiff.Substring(err, tt.wantErr); s != "" {
				t.Fatalf("lineCards() unexpected error: %s", s)
			}
			if diff := cmp.Diff(tt.want, got); diff != "" {
				t.Errorf("lineCards() mismatch (-want +got):\n%s", diff)
			}
			// The result is set on an unstructured object, so it must be deep copyable.
			if got != nil {
				obj := map[string]any{}
				if err := unstructured.SetNestedSlice(obj, got, "spec", "components"); err != nil {
					t.Errorf("SetNestedSlice() failed: %v", err)
				}
			}
		})
	}
}
