package kubeadm

import (
	"os"
	"strings"
	"testing"

	"github.com/openconfig/gnmi/errdiff"
	kexec "github.com/openconfig/kne/exec"
	fexec "github.com/openconfig/kne/exec/fake"
)

func TestEnableCredentialProvider(t *testing.T) {
	f, err := os.CreateTemp(t.TempDir(), "flags.env")
	if err != nil {
		t.Fatalf("Failed to create temp file for test: %v", err)
	}
	if _, err := f.WriteString("KUBELET_KUBEADM_ARGS=\"--container-runtime-endpoint=unix:///var/run/cri-dockerd.sock\"\n"); err != nil {
		t.Fatalf("Failed to write temp file for test: %v", err)
	}

	origKubeadmFlagPath := kubeadmFlagPath
	defer func() {
		kubeadmFlagPath = origKubeadmFlagPath
	}()
	kubeadmFlagPath = f.Name()

	cfg, err := os.CreateTemp(t.TempDir(), "cfg.yaml")
	if err != nil {
		t.Fatalf("Failed to create temp cfg file for test: %v", err)
	}

	tests := []struct {
		desc    string
		cfgPath string
		resp    []fexec.Response
		wantErr string
	}{{
		desc:    "success",
		cfgPath: cfg.Name(),
		resp: []fexec.Response{
			{Cmd: "sudo", Args: []string{"kubeadm", "upgrade", "node", "phase", "kubelet-config"}},
			{Cmd: "sudo", Args: []string{"cp", "-f", ".*", kubeadmFlagPath}},
			{Cmd: "sudo", Args: []string{"systemctl", "restart", "kubelet"}},
		},
	}, {
		desc:    "config file not found",
		cfgPath: "nonexistent",
		wantErr: "config file not found",
	}, {
		desc:    "failed to upgrade kubelet",
		cfgPath: cfg.Name(),
		resp: []fexec.Response{
			{Cmd: "sudo", Args: []string{"kubeadm", "upgrade", "node", "phase", "kubelet-config"}, Err: "failed to upgrade kubelet"},
		},
		wantErr: "failed to upgrade kubelet",
	}, {
		desc:    "failed to copy flag config",
		cfgPath: cfg.Name(),
		resp: []fexec.Response{
			{Cmd: "sudo", Args: []string{"kubeadm", "upgrade", "node", "phase", "kubelet-config"}},
			{Cmd: "sudo", Args: []string{"cp", "-f", ".*", kubeadmFlagPath}, Err: "failed to copy"},
		},
		wantErr: "failed to copy",
	}, {
		desc:    "failed to restart kubelet",
		cfgPath: cfg.Name(),
		resp: []fexec.Response{
			{Cmd: "sudo", Args: []string{"kubeadm", "upgrade", "node", "phase", "kubelet-config"}},
			{Cmd: "sudo", Args: []string{"cp", "-f", ".*", kubeadmFlagPath}},
			{Cmd: "sudo", Args: []string{"systemctl", "restart", "kubelet"}, Err: "failed to restart kubelet"},
		},
		wantErr: "failed to restart kubelet",
	}}
	for _, tt := range tests {
		t.Run(tt.desc, func(t *testing.T) {
			fexec.LogCommand = func(s string) {
				t.Logf("%s: %s", tt.desc, s)
			}
			cmds := fexec.Commands(tt.resp)
			kexec.Command = cmds.Command
			defer checkCmds(t, cmds)

			err := EnableCredentialProvider(tt.cfgPath)
			if s := errdiff.Substring(err, tt.wantErr); s != "" {
				t.Fatalf("unexpected error: %s", s)
			}
		})
	}
}

func checkCmds(t *testing.T, cmds *fexec.Command) {
	t.Helper()
	if err := cmds.Done(); err != nil {
		t.Errorf("%v", err)
	}
}

func TestCreateInitConfigFile(t *testing.T) {
	tests := []struct {
		desc             string
		opts             InitConfigOptions
		wantInitCfg      bool
		wantYAMLContains []string
	}{{
		desc: "defaults",
		opts: InitConfigOptions{
			ImageRepository:      "us-west1-docker.pkg.dev/kne-external/kne",
			ServiceNodePortRange: "10000-32767",
		},
		wantInitCfg: false,
		wantYAMLContains: []string{
			"apiVersion: kubeadm.k8s.io/v1beta3",
			"kind: ClusterConfiguration",
			"imageRepository: us-west1-docker.pkg.dev/kne-external/kne",
			"service-node-port-range: 10000-32767",
		},
	}, {
		desc: "with CRI socket and pod network CIDR and token TTL",
		opts: InitConfigOptions{
			CRISocket:            "unix:///var/run/containerd/containerd.sock",
			PodNetworkCIDR:       "10.244.0.0/16",
			TokenTTL:             "0",
			ImageRepository:      "registry.k8s.io",
			ServiceNodePortRange: "20000-30000",
		},
		wantInitCfg: true,
		wantYAMLContains: []string{
			"kind: InitConfiguration",
			"criSocket: unix:///var/run/containerd/containerd.sock",
			"ttl: \"0\"",
			"kind: ClusterConfiguration",
			"imageRepository: registry.k8s.io",
			"podSubnet: 10.244.0.0/16",
			"service-node-port-range: 20000-30000",
		},
	}}

	for _, tt := range tests {
		t.Run(tt.desc, func(t *testing.T) {
			path, cleanup, err := CreateInitConfigFile(tt.opts)
			if err != nil {
				t.Fatalf("CreateInitConfigFile failed: %v", err)
			}
			defer cleanup()

			b, err := os.ReadFile(path)
			if err != nil {
				t.Fatalf("Failed to read created config file: %v", err)
			}
			content := string(b)
			if tt.wantInitCfg && !strings.Contains(content, "kind: InitConfiguration") {
				t.Errorf("Expected InitConfiguration document in config, got:\n%s", content)
			}
			if !tt.wantInitCfg && strings.Contains(content, "kind: InitConfiguration") {
				t.Errorf("Unexpected InitConfiguration document in config, got:\n%s", content)
			}
			for _, want := range tt.wantYAMLContains {
				if !strings.Contains(content, want) {
					t.Errorf("Config file missing %q, got:\n%s", want, content)
				}
			}
		})
	}
}
