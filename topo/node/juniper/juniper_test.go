// Juniper cPTX for KNE
// Copyright (c) Juniper Networks, Inc., 2021. All rights reserved.

package juniper

import (
	"bufio"
	"bytes"
	"context"
	"fmt"
	"io"
	"os"
	"regexp"
	"testing"
	"time"

	"github.com/google/go-cmp/cmp"
	"github.com/openconfig/gnmi/errdiff"
	tpb "github.com/openconfig/kne/proto/topo"
	"github.com/openconfig/kne/topo/node"
	scrapliopts "github.com/scrapli/scrapligo/driver/options"
	scraplilogging "github.com/scrapli/scrapligo/logging"
	scraplitransport "github.com/scrapli/scrapligo/transport"
	scrapliutil "github.com/scrapli/scrapligo/util"
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

// removeCommentsFromConfig removes comment lines from a JunOS config file
// and returns the remaining config in an io.Reader.
// Using scrapli_cfg_testing results in an EOF error when config includes comments.
// Comments in config files are not problematic when using kne (not testing).
// This is a simple implementation that only removes lines that are entirely comments.
func removeCommentsFromConfig(t *testing.T, r io.Reader) io.Reader {
	t.Helper()
	var buf bytes.Buffer
	br := bufio.NewReader(r)
	re := regexp.MustCompile(`^\s*(?:(?:\/\*)|[#\*])`)
	for {
		line, err := br.ReadBytes('\n')
		if err != nil && err != io.EOF {
			t.Fatalf("br.ReadBytes() failed: %+v\n", err)
		}

		if re.Find(line) == nil {
			fmt.Fprint(&buf, string(line))
		}

		if err == io.EOF {
			break
		}
	}
	return &buf
}

func TestGenerateSelfSigned(t *testing.T) {
	ki := fake.NewSimpleClientset(&corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name: "pod1",
		},
	})

	reaction := func(action ktest.Action) (handled bool, ret watch.Interface, err error) {
		f := &fakeWatch{
			e: []watch.Event{
				{
					// Test that watcher properly handles events with the wrong type.
					Object: &corev1.ConfigMap{},
				},
				{
					Object: &corev1.Pod{
						Status: corev1.PodStatus{
							Phase: corev1.PodRunning,
						},
					},
				},
			},
		}
		return true, f, nil
	}
	ki.PrependWatchReactor("*", reaction)

	ni := &node.Impl{
		KubeClient: ki,
		Namespace:  "test",
		Proto: &tpb.Node{
			Name:   "pod1",
			Vendor: tpb.Vendor_JUNIPER,
			Config: &tpb.Config{
				Cert: &tpb.CertificateCfg{
					Config: &tpb.CertificateCfg_SelfSigned{
						SelfSigned: &tpb.SelfSignedCertCfg{
							CertName: "grpc-server-cert",
							KeyName:  "my_key",
							KeySize:  2048,
						},
					},
				},
			},
		},
	}

	origCertGenRetrySleep := certGenRetrySleep
	defer func() {
		certGenRetrySleep = origCertGenRetrySleep
	}()
	certGenRetrySleep = time.Millisecond

	origConfigModeRetrySleep := configModeRetrySleep
	defer func() {
		configModeRetrySleep = origConfigModeRetrySleep
	}()
	configModeRetrySleep = time.Millisecond

	origCertGenTimeout := certGenTimeout
	defer func() {
		certGenTimeout = origCertGenTimeout
	}()
	certGenTimeout = time.Second * 10

	origConfigModeTimeout := configModeTimeout
	defer func() {
		configModeTimeout = origConfigModeTimeout
	}()
	configModeTimeout = time.Second * 10

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
		{
			// device returns config mode error but we eventually recover
			desc:     "success config mode",
			wantErr:  false,
			ni:       ni,
			testFile: "testdata/generate_certificate_config_mode_success",
		},
		{
			// device returns "Error: something bad happened" -- we expect to fail
			desc:     "failure config commit",
			wantErr:  true,
			ni:       ni,
			testFile: "testdata/generate_certificate_config_mode_failure",
		},
	}

	for _, tt := range tests {
		t.Run(tt.desc, func(t *testing.T) {
			nImpl, err := New(tt.ni)

			if err != nil {
				t.Fatalf("failed creating kne juniper cptx node")
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
					t.Fatalf("failed created scrapligo logger %v", err)
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

func TestGRPCConfig(t *testing.T) {
	tests := []struct {
		desc string
		ni   *node.Impl
		want []string
	}{
		{
			desc: "new grpc server config",
			ni: &node.Impl{
				KubeClient: fake.NewSimpleClientset(),
				Namespace:  "test",
				Proto: &tpb.Node{
					Name:   "pod1",
					Vendor: tpb.Vendor_JUNIPER,
					Config: &tpb.Config{
						ConfigFile: "foo",
						ConfigPath: "/",
						ConfigData: &tpb.Config_Data{
							Data: []byte("config file data"),
						},
					},
					Labels: map[string]string{
						"legacy_grpc_server_config": "disabled",
					},
				},
			},
			want: []string{
				"set system services http servers server grpc-server",
				"set system services http servers server grpc-server port 32767",
				"set system services http servers server grpc-server grpc all-grpc",
				"set system services http servers server grpc-server tls local-certificate grpc-server-cert",
				"set system services http servers server grpc-server listen-address 0.0.0.0",
				"commit",
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.desc, func(t *testing.T) {
			nImpl, err := New(tt.ni)
			if err != nil {
				t.Fatalf("failed creating kne juniper node")
			}
			n, _ := nImpl.(*Node)
			got := n.GRPCConfig()
			if diff := cmp.Diff(tt.want, got); diff != "" {
				t.Errorf("GRPCConfig() returned unexpected diff (-want +got):\n%s", diff)
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
		Vendor: tpb.Vendor_JUNIPER,
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
			testFile: "testdata/config_push_success",
			testConf: "testdata/ncptx-config",
		},
		{
			// We encounter unexpected response -- we expect to fail
			desc:    "failure",
			wantErr: true,
			ni: &node.Impl{
				KubeClient: ki,
				Namespace:  "test",
				Proto:      validPb,
			},
			testFile: "testdata/config_push_failure",
			testConf: "testdata/ncptx-config",
		},
	}

	for _, tt := range tests {
		t.Run(tt.desc, func(t *testing.T) {
			nImpl, err := New(tt.ni)
			if err != nil {
				t.Fatalf("failed creating kne juniper node")
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

			fp, err := os.Open(tt.testConf)
			if err != nil {
				t.Fatalf("unable to open file, error: %+v\n", err)
			}
			defer fp.Close()

			ctx := context.Background()
			fbuf := removeCommentsFromConfig(t, fp)

			err = n.ConfigPush(ctx, fbuf)
			if err != nil && !tt.wantErr {
				t.Fatalf("config push test failed, error: %+v\n", err)
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
		Proto: &tpb.Node{
			Name:   "pod1",
			Vendor: tpb.Vendor_JUNIPER,
			Config: &tpb.Config{
				Cert: &tpb.CertificateCfg{
					Config: &tpb.CertificateCfg_SelfSigned{
						SelfSigned: &tpb.SelfSignedCertCfg{
							CertName: "grpc-server-cert",
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
			// successfully reset config
			desc:     "success",
			wantErr:  false,
			ni:       ni,
			testFile: "testdata/config_reset_success",
		},
		{
			// device returns "Error: something bad happened" -- we expect to fail
			desc:     "failure",
			wantErr:  true,
			ni:       ni,
			testFile: "testdata/config_reset_failure",
		},
	}

	for _, tt := range tests {
		t.Run(tt.desc, func(t *testing.T) {
			nImpl, err := New(tt.ni)

			if err != nil {
				t.Fatalf("failed creating kne juniper ncptx node")
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
					t.Fatalf("failed created scrapligo logger %v", err)
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
		want: &tpb.Node{
			Name:  "pod1",
			Model: "ncptx",
			Os:    "evo",
			Constraints: map[string]string{
				"cpu":    "4000m",
				"memory": "4Gi",
			},
			Services: map[uint32]*tpb.Service{
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
					Inside: 32767,
				},
				9340: {
					Names:  []string{"gribi"},
					Inside: 32767,
				},
				9559: {
					Names:  []string{"p4rt"},
					Inside: 32767,
				},
			},
			Labels: map[string]string{
				"vendor":       tpb.Vendor_JUNIPER.String(),
				"ondatra-role": "DUT",
				"model":        "ncptx",
				"os":           "evo",
			},
			Config: &tpb.Config{
				Image: "ncptx:latest",
				Command: []string{
					"/sbin/cevoCntrEntryPoint",
				},
				Env: map[string]string{
					"JUNOS_EVOLVED_CONTAINER": "1",
				},
				EntryCommand: "kubectl exec -it pod1 -- cli",
				ConfigPath:   "/home/evo/configdisk",
				ConfigFile:   "juniper.conf",
				Cert: &tpb.CertificateCfg{
					Config: &tpb.CertificateCfg_SelfSigned{
						SelfSigned: &tpb.SelfSignedCertCfg{
							CertName: "grpc-server-cert",
							KeyName:  "my_key",
							KeySize:  2048,
						},
					},
				},
			},
		},
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
			},
		},
		want: &tpb.Node{
			Name:  "pod1",
			Model: "ncptx",
			Os:    "evo",
			Constraints: map[string]string{
				"cpu":    "4000m",
				"memory": "4Gi",
			},
			Services: map[uint32]*tpb.Service{
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
					Inside: 32767,
				},
				9340: {
					Names:  []string{"gribi"},
					Inside: 32767,
				},
				9559: {
					Names:  []string{"p4rt"},
					Inside: 32767,
				},
			},
			Labels: map[string]string{
				"vendor":       tpb.Vendor_JUNIPER.String(),
				"ondatra-role": "DUT",
				"model":        "ncptx",
				"os":           "evo",
			},
			Config: &tpb.Config{
				Image: "ncptx:latest",
				Command: []string{
					"/sbin/cevoCntrEntryPoint",
				},
				Env: map[string]string{
					"JUNOS_EVOLVED_CONTAINER": "1",
				},
				EntryCommand: "kubectl exec -it pod1 -- cli",
				ConfigPath:   "/",
				ConfigFile:   "foo",
				ConfigData: &tpb.Config_Data{
					Data: []byte("config file data"),
				},
				Cert: &tpb.CertificateCfg{
					Config: &tpb.CertificateCfg_SelfSigned{
						SelfSigned: &tpb.SelfSignedCertCfg{
							CertName: "grpc-server-cert",
							KeyName:  "my_key",
							KeySize:  2048,
						},
					},
				},
			},
		},
	}, {
		desc: "full proto cptx",
		ni: &node.Impl{
			KubeClient: fake.NewSimpleClientset(),
			Namespace:  "test",
			Proto: &tpb.Node{
				Name:  "pod1",
				Model: "cptx",
				Config: &tpb.Config{
					ConfigFile: "foo",
					ConfigPath: "/",
					ConfigData: &tpb.Config_Data{
						Data: []byte("config file data"),
					},
				},
			},
		},
		want: &tpb.Node{
			Name:  "pod1",
			Os:    "evo",
			Model: "cptx",
			Constraints: map[string]string{
				"cpu":    "8000m",
				"memory": "8Gi",
			},
			Services: map[uint32]*tpb.Service{
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
					Inside: 32767,
				},
				9340: {
					Names:  []string{"gribi"},
					Inside: 32767,
				},
				9559: {
					Names:  []string{"p4rt"},
					Inside: 32767,
				},
			},
			Labels: map[string]string{
				"vendor":       tpb.Vendor_JUNIPER.String(),
				"ondatra-role": "DUT",
				"model":        "cptx",
				"os":           "evo",
			},
			Config: &tpb.Config{
				Image: "cptx:latest",
				Command: []string{
					"/entrypoint.sh",
				},
				Env: map[string]string{
					"JUNOS_EVOLVED_CONTAINER": "1",
				},
				EntryCommand: "kubectl exec -it pod1 -- cli",
				ConfigPath:   "/",
				ConfigFile:   "foo",
				ConfigData: &tpb.Config_Data{
					Data: []byte("config file data"),
				},
				Cert: &tpb.CertificateCfg{
					Config: &tpb.CertificateCfg_SelfSigned{
						SelfSigned: &tpb.SelfSignedCertCfg{
							CertName: "grpc-server-cert",
							KeyName:  "my_key",
							KeySize:  2048,
						},
					},
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
			Model: "ncptx",
			Os:    "evo",
			Constraints: map[string]string{
				"cpu":    "4000m",
				"memory": "4Gi",
			},
			Services: map[uint32]*tpb.Service{
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
					Inside: 32767,
				},
				9340: {
					Names:  []string{"gribi"},
					Inside: 32767,
				},
				9559: {
					Names:  []string{"p4rt"},
					Inside: 32767,
				},
			},
			Labels: map[string]string{
				"vendor":       tpb.Vendor_JUNIPER.String(),
				"ondatra-role": "DUT",
				"model":        "ncptx",
				"os":           "evo",
			},
			Config: &tpb.Config{
				Image: "ncptx:latest",
				Command: []string{
					"/sbin/cevoCntrEntryPoint",
				},
				Env: map[string]string{
					"JUNOS_EVOLVED_CONTAINER": "1",
				},
				EntryCommand: "kubectl exec -it  -- cli",
				ConfigPath:   "/home/evo/configdisk",
				ConfigFile:   "juniper.conf",
				Cert: &tpb.CertificateCfg{
					Config: &tpb.CertificateCfg_SelfSigned{
						SelfSigned: &tpb.SelfSignedCertCfg{
							CertName: "grpc-server-cert",
							KeyName:  "my_key",
							KeySize:  2048,
						},
					},
				},
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
			if s := cmp.Diff(tt.want, n.GetProto(), protocmp.Transform(), protocmp.IgnoreFields(&tpb.Service{}, "node_port")); s != "" {
				t.Fatalf("New() failed: diff (-want, +got): \n%s", s)
			}
			err = n.Create(context.Background())
			if s := errdiff.Check(err, tt.cErr); s != "" {
				t.Fatalf("Unexpected error: %s", s)
			}
		})
	}
}

func TestDefaultNodeConstraints(t *testing.T) {
	tests := []struct {
		name       string
		node       *Node
		wantCPU    string
		wantMemory string
	}{
		{
			name:       "Case: Node.Impl is nil",
			node:       &Node{Impl: nil},
			wantCPU:    defaultNCPTXConstraints.CPU,
			wantMemory: defaultNCPTXConstraints.Memory,
		},
		{
			name:       "Case: Node.Impl.Proto is nil",
			node:       &Node{Impl: &node.Impl{Proto: nil}},
			wantCPU:    defaultNCPTXConstraints.CPU,
			wantMemory: defaultNCPTXConstraints.Memory,
		},
		{
			name: "Case: Model is cptx",
			node: &Node{
				Impl: &node.Impl{
					Proto: &tpb.Node{Model: "cptx"},
				},
			},
			wantCPU:    defaultCPTXConstraints.CPU,
			wantMemory: defaultCPTXConstraints.Memory,
		},
		{
			name: "Case: Model is empty string",
			node: &Node{
				Impl: &node.Impl{
					Proto: &tpb.Node{},
				},
			},
			wantCPU:    defaultNCPTXConstraints.CPU,
			wantMemory: defaultNCPTXConstraints.Memory,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			constraints := tt.node.DefaultNodeConstraints()
			if constraints.CPU != tt.wantCPU {
				t.Errorf("DefaultNodeConstraints() returned unexpected CPU: got %s, want %s", constraints.CPU, tt.wantCPU)
			}

			if constraints.Memory != tt.wantMemory {
				t.Errorf("DefaultNodeConstraints() returned unexpected Memory: got %s, want %s", constraints.Memory, tt.wantMemory)
			}
		})
	}
}
