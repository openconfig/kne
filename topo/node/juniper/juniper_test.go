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
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
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

// scrapliDebug checks if SCRAPLI_DEBUG env var is set.
// used in testing to enable debug log of scrapligo.
func scrapliDebug() bool {
	_, set := os.LookupEnv("SCRAPLI_DEBUG")

	return set
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
		fw := watch.NewFakeWithChanSize(2, false)
		// Test that watcher properly handles events with the wrong type.
		fw.Add(&corev1.ConfigMap{})
		fw.Add(&corev1.Pod{
			Status: corev1.PodStatus{
				Phase: corev1.PodRunning,
			},
		})
		return true, fw, nil
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
	certGenTimeout = time.Second * 2

	origConfigModeTimeout := configModeTimeout
	defer func() {
		configModeTimeout = origConfigModeTimeout
	}()
	configModeTimeout = time.Second * 2

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
		{
			// nil kubeclient
			desc:    "nil kubeclient",
			wantErr: true,
			ni: &node.Impl{
				Namespace: "test",
				Proto:     ni.Proto,
			},
		},
		{
			// pod already running
			desc:     "pod already running",
			wantErr:  false,
			testFile: "testdata/generate_certificate_success",
			ni: &node.Impl{
				KubeClient: fake.NewSimpleClientset(&corev1.Pod{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "pod1",
						Namespace: "test",
					},
					Status: corev1.PodStatus{
						Phase: corev1.PodRunning,
					},
				}),
				Namespace: "test",
				Proto:     ni.Proto,
			},
		},
		{
			// invalid cert name contains invalid characters
			desc:     "invalid cert name characters",
			wantErr:  true,
			testFile: "testdata/generate_certificate_failure",
			ni: &node.Impl{
				KubeClient: ki,
				Namespace:  "test",
				Proto: &tpb.Node{
					Name:   "pod1",
					Vendor: tpb.Vendor_JUNIPER,
					Config: &tpb.Config{
						Cert: &tpb.CertificateCfg{
							Config: &tpb.CertificateCfg_SelfSigned{
								SelfSigned: &tpb.SelfSignedCertCfg{
									CertName: "grpc-cert;invalid",
								},
							},
						},
					},
				},
			},
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
				"set system services http servers server grpc-server-9339",
				"set system services http servers server grpc-server-9339 port 9339",
				"set system services http servers server grpc-server-9339 grpc gnmi",
				"set system services http servers server grpc-server-9339 grpc gnoi",
				"set system services http servers server grpc-server-9339 grpc gnsi",
				"set system services http servers server grpc-server-9339 tls local-certificate grpc-server-cert",
				"set system services http servers server grpc-server-9339 listen-address 0.0.0.0",
				"set system services http servers server grpc-server-9339 grpc all-grpc max-connections 300",
				"set system services http servers server grpc-server-9340",
				"set system services http servers server grpc-server-9340 port 9340",
				"set system services http servers server grpc-server-9340 grpc gribi",
				"set system services http servers server grpc-server-9340 tls local-certificate grpc-server-cert",
				"set system services http servers server grpc-server-9340 listen-address 0.0.0.0",
				"set system services http servers server grpc-server-9340 grpc all-grpc max-connections 300",
				"set system services http servers server grpc-server-9559",
				"set system services http servers server grpc-server-9559 port 9559",
				"set system services http servers server grpc-server-9559 grpc p4",
				"set system services http servers server grpc-server-9559 tls local-certificate grpc-server-cert",
				"set system services http servers server grpc-server-9559 listen-address 0.0.0.0",
				"set system services http servers server grpc-server-9559 grpc all-grpc max-connections 300",
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
		fw := watch.NewFakeWithChanSize(1, false)
		fw.Add(&corev1.Pod{
			Status: corev1.PodStatus{
				Phase: corev1.PodRunning,
			},
		})
		return true, fw, nil
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
		fw := watch.NewFakeWithChanSize(1, false)
		fw.Add(&corev1.Pod{
			Status: corev1.PodStatus{
				Phase: corev1.PodRunning,
			},
		})
		return true, fw, nil
	}
	ki.PrependWatchReactor("*", reaction)

	origConfigModeRetrySleep := configModeRetrySleep
	defer func() {
		configModeRetrySleep = origConfigModeRetrySleep
	}()
	configModeRetrySleep = time.Millisecond

	origConfigModeTimeout := configModeTimeout
	defer func() {
		configModeTimeout = origConfigModeTimeout
	}()
	configModeTimeout = 100 * time.Millisecond

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
					Inside: 9339,
				},
				9340: {
					Names:  []string{"gribi"},
					Inside: 9340,
				},
				9559: {
					Names:  []string{"p4rt"},
					Inside: 9559,
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
					Inside: 9339,
				},
				9340: {
					Names:  []string{"gribi"},
					Inside: 9340,
				},
				9559: {
					Names:  []string{"p4rt"},
					Inside: 9559,
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
					Inside: 9339,
				},
				9340: {
					Names:  []string{"gribi"},
					Inside: 9340,
				},
				9559: {
					Names:  []string{"p4rt"},
					Inside: 9559,
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
					Inside: 9339,
				},
				9340: {
					Names:  []string{"gribi"},
					Inside: 9340,
				},
				9559: {
					Names:  []string{"p4rt"},
					Inside: 9559,
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

func TestValidCertNameRegexp(t *testing.T) {
	tests := []struct {
		certName  string
		wantValid bool
	}{
		{"grpc-server-cert", true},
		{"my_cert.v1-2", true},
		{"cert;invalid", false},
		{"cert$invalid", false},
		{"cert/invalid", false},
		{"cert name spaces", false},
	}

	for _, tt := range tests {
		t.Run(tt.certName, func(t *testing.T) {
			got := validCertNameRegexp.MatchString(tt.certName)
			if got != tt.wantValid {
				t.Errorf("validCertNameRegexp.MatchString(%q) = %v, want %v", tt.certName, got, tt.wantValid)
			}
		})
	}
}

func TestCreate(t *testing.T) {
	tests := []struct {
		desc              string
		configData        []byte
		wantInitCommand   []string
		wantInitArgsLen   int
		wantInitMountsLen int
		wantMainMountsLen int
		wantInitScriptSub []string
		wantErr           bool
	}{
		{
			desc:              "cPTX with config data",
			configData:        []byte("set system host-name ncptx"),
			wantInitCommand:   []string{"/bin/sh", "-c"},
			wantInitArgsLen:   4, // script, "init", num_intfs, sleep
			wantInitMountsLen: 2, // /config-dst and /config-src
			wantMainMountsLen: 5, // 4 base mounts (/run, /tmp, /dev/shm, /sys/fs/cgroup) + config mount
			wantInitScriptSub: []string{"/entrypoint.sh", "re0:mgmt-0 unit 0 family inet", "routing-options static route 0.0.0.0/0", "/config-dst/juniper.conf"},
		},
		{
			desc:              "cPTX without config data",
			configData:        nil,
			wantInitCommand:   []string{"/bin/sh", "-c"},
			wantInitArgsLen:   4,
			wantInitMountsLen: 1, // only /config-dst
			wantMainMountsLen: 5,
			wantInitScriptSub: []string{"/entrypoint.sh", "re0:mgmt-0 unit 0 family inet", "routing-options static route 0.0.0.0/0"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.desc, func(t *testing.T) {
			ki := fake.NewSimpleClientset()
			cfg := &tpb.Config{
				ConfigFile: "juniper.conf",
				ConfigPath: "/home/evo/configdisk",
			}
			if tt.configData != nil {
				cfg.ConfigData = &tpb.Config_Data{Data: tt.configData}
			}
			ni := &node.Impl{
				Namespace:  "test",
				KubeClient: ki,
				Proto: &tpb.Node{
					Name:       "ncptx",
					Model:      "ncptx",
					Vendor:     tpb.Vendor_JUNIPER,
					Config:     cfg,
					Interfaces: map[string]*tpb.Interface{
						"eth1": {Name: "et-0/0/0:0"},
					},
				},
			}
			n, err := New(ni)
			if err != nil {
				t.Fatalf("New() unexpected error = %v", err)
			}
			if err := n.Create(context.Background()); (err != nil) != tt.wantErr {
				t.Fatalf("Create() unexpected error = %v, wantErr = %v", err, tt.wantErr)
			}
			pod, err := ki.CoreV1().Pods("test").Get(context.Background(), "ncptx", metav1.GetOptions{})
			if err != nil {
				t.Fatalf("failed to get created pod: %v", err)
			}
			if len(pod.Spec.InitContainers) != 1 {
				t.Fatalf("expected 1 init container, got %d", len(pod.Spec.InitContainers))
			}
			initC := pod.Spec.InitContainers[0]
			if diff := cmp.Diff(tt.wantInitCommand, initC.Command); diff != "" {
				t.Errorf("init container command diff (-want +got):\n%s", diff)
			}
			if len(initC.Args) != tt.wantInitArgsLen {
				t.Errorf("init container args len = %d, want %d", len(initC.Args), tt.wantInitArgsLen)
			}
			if len(initC.VolumeMounts) != tt.wantInitMountsLen {
				t.Errorf("init container volume mounts len = %d, want %d", len(initC.VolumeMounts), tt.wantInitMountsLen)
			}
			if len(pod.Spec.Containers[0].VolumeMounts) != tt.wantMainMountsLen {
				t.Errorf("main container volume mounts len = %d, want %d", len(pod.Spec.Containers[0].VolumeMounts), tt.wantMainMountsLen)
			}
			for _, sub := range tt.wantInitScriptSub {
				if !strings.Contains(initC.Args[0], sub) {
					t.Errorf("init container script missing expected substring %q", sub)
				}
			}
		})
	}
}

func TestJuniperInitScriptExecution(t *testing.T) {
	tmpDir := t.TempDir()
	srcDir := filepath.Join(tmpDir, "config-src")
	dstDir := filepath.Join(tmpDir, "config-dst")
	binDir := filepath.Join(tmpDir, "bin")
	if err := os.MkdirAll(srcDir, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(binDir, 0755); err != nil {
		t.Fatal(err)
	}

	configFile := "juniper.conf"
	srcFile := filepath.Join(srcDir, configFile)
	if err := os.WriteFile(srcFile, []byte("set system host-name ncptx\n"), 0644); err != nil {
		t.Fatal(err)
	}

	// Create fake entrypoint.sh in binDir
	entrypointPath := filepath.Join(binDir, "entrypoint.sh")
	if err := os.WriteFile(entrypointPath, []byte("#!/bin/sh\nexit 0\n"), 0755); err != nil {
		t.Fatal(err)
	}

	// Create fake ip command in binDir
	ipPath := filepath.Join(binDir, "ip")
	ipScript := `#!/bin/sh
if [ "$1" = "-4" ] && [ "$2" = "addr" ]; then
  echo "    inet 10.244.0.15/24 scope global eth0"
elif [ "$1" = "-4" ] && [ "$2" = "route" ]; then
  echo "default via 10.244.0.1 dev eth0"
elif [ "$1" = "-6" ] && [ "$2" = "addr" ]; then
  echo "    inet6 2001:db8::15/64 scope global"
elif [ "$1" = "-6" ] && [ "$2" = "route" ]; then
  echo "default via 2001:db8::1 dev eth0"
fi
`
	if err := os.WriteFile(ipPath, []byte(ipScript), 0755); err != nil {
		t.Fatal(err)
	}

	initScript := fmt.Sprintf(`
%[4]s "$1" "$2"
mkdir -p %[1]s
if [ -f %[2]s/%[3]s ]; then
  cp %[2]s/%[3]s %[1]s/%[3]s
else
  touch %[1]s/%[3]s
fi
IP4=$(ip -4 addr show dev eth0 2>/dev/null | awk '/inet /{print $2}' | head -n1)
GW4=$(ip -4 route show default 2>/dev/null | awk '{print $3}' | head -n1)
if [ -n "$IP4" ]; then
  printf '\nset interfaces re0:mgmt-0 unit 0 family inet address %%s\n' "$IP4" >> %[1]s/%[3]s
fi
if [ -n "$GW4" ]; then
  printf 'set routing-options static route 0.0.0.0/0 next-hop %%s\n' "$GW4" >> %[1]s/%[3]s
fi
IP6=$(ip -6 addr show dev eth0 2>/dev/null | awk '/inet6 /{print $2}' | grep -v '^fe80' | head -n1)
GW6=$(ip -6 route show default 2>/dev/null | awk '{print $3}' | head -n1)
if [ -n "$IP6" ]; then
  printf '\nset interfaces re0:mgmt-0 unit 0 family inet6 address %%s\n' "$IP6" >> %[1]s/%[3]s
fi
if [ -n "$GW6" ]; then
  printf 'set routing-options rib inet6.0 static route ::/0 next-hop %%s\n' "$GW6" >> %[1]s/%[3]s
fi
`, dstDir, srcDir, configFile, entrypointPath)

	cmd := exec.Command("/bin/sh", "-c", initScript, "init", "2", "0")
	cmd.Env = append(os.Environ(), "PATH="+binDir+":"+os.Getenv("PATH"))
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("script execution failed: %v, output: %s", err, string(out))
	}

	dstFile := filepath.Join(dstDir, configFile)
	content, err := os.ReadFile(dstFile)
	if err != nil {
		t.Fatalf("failed to read generated config: %v", err)
	}

	got := string(content)
	wantContains := []string{
		"set system host-name ncptx",
		"set interfaces re0:mgmt-0 unit 0 family inet address 10.244.0.15/24",
		"set routing-options static route 0.0.0.0/0 next-hop 10.244.0.1",
		"set interfaces re0:mgmt-0 unit 0 family inet6 address 2001:db8::15/64",
		"set routing-options rib inet6.0 static route ::/0 next-hop 2001:db8::1",
	}
	for _, want := range wantContains {
		if !strings.Contains(got, want) {
			t.Errorf("generated config missing %q\nGot:\n%s", want, got)
		}
	}
}
