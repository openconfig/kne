package kubeadm

import (
	"os"
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
			{Cmd: "sudo", Args: []string{"cp", ".*", kubeadmFlagPath}},
			{Cmd: "sudo", Args: []string{"systemctl", "restart", "kubelet"}},
		},
	}, {
		desc:    "config file not found",
		cfgPath: "dne",
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
			{Cmd: "sudo", Args: []string{"cp", ".*", kubeadmFlagPath}, Err: "failed to copy"},
		},
		wantErr: "failed to copy",
	}, {
		desc:    "failed to restart kubelet",
		cfgPath: cfg.Name(),
		resp: []fexec.Response{
			{Cmd: "sudo", Args: []string{"kubeadm", "upgrade", "node", "phase", "kubelet-config"}},
			{Cmd: "sudo", Args: []string{"cp", ".*", kubeadmFlagPath}},
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
