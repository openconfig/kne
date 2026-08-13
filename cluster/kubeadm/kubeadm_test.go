package kubeadm

import (
	"os"
	"path/filepath"
	"testing"
	"time"

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

func TestSetServiceNodePortRange(t *testing.T) {
	origKubeAPIServerManifest := kubeAPIServerManifest
	origSleepFn := sleepFn
	origTimeout := apiserverWaitTimeout
	origPollInterval := apiserverPollInterval
	defer func() {
		kubeAPIServerManifest = origKubeAPIServerManifest
		sleepFn = origSleepFn
		apiserverWaitTimeout = origTimeout
		apiserverPollInterval = origPollInterval
	}()
	sleepFn = func(time.Duration) {}
	apiserverWaitTimeout = 50 * time.Millisecond
	apiserverPollInterval = time.Millisecond

	manifestWithClusterIP := `apiVersion: v1
kind: Pod
metadata:
  name: kube-apiserver
spec:
  containers:
  - command:
    - kube-apiserver
    - --service-cluster-ip-range=10.96.0.0/12
    - --advertise-address=192.168.1.10
`
	manifestWithKubeAPIServer := `apiVersion: v1
kind: Pod
metadata:
  name: kube-apiserver
spec:
  containers:
  - command:
    - kube-apiserver
    - --advertise-address=192.168.1.10
`
	manifestWithExistingRange := `apiVersion: v1
kind: Pod
metadata:
  name: kube-apiserver
spec:
  containers:
  - command:
    - kube-apiserver
    - --service-node-port-range=10000-32767
`
	manifestNoMatch := `apiVersion: v1
kind: Pod
metadata:
  name: other-pod
spec:
  containers:
  - command:
    - other-command
`

	tests := []struct {
		desc         string
		manifestData string
		nonExistent  bool
		portRange    string
		waitTimeout  time.Duration
		resp         []fexec.Response
		wantErr      string
	}{{
		desc:         "success with service-cluster-ip-range target",
		manifestData: manifestWithClusterIP,
		portRange:    "10000-32767",
		resp: []fexec.Response{
			{Cmd: "sudo", Args: []string{"cp", "-f", ".*", ".*"}},
			{Cmd: "kubectl", Args: []string{"get", "pod", "-n", "kube-system", "-l", "component=kube-apiserver", "-o", ".*"}, Stdout: "--service-node-port-range=10000-32767"},
			{Cmd: "kubectl", Args: []string{"get", "--raw", "/readyz"}, Stdout: "ok"},
		},
	}, {
		desc:         "success with kube-apiserver fallback target",
		manifestData: manifestWithKubeAPIServer,
		portRange:    "10000-32767",
		resp: []fexec.Response{
			{Cmd: "sudo", Args: []string{"cp", "-f", ".*", ".*"}},
			{Cmd: "kubectl", Args: []string{"get", "pod", "-n", "kube-system", "-l", "component=kube-apiserver", "-o", ".*"}, Stdout: "--service-node-port-range=10000-32767"},
			{Cmd: "kubectl", Args: []string{"get", "--raw", "/readyz"}, Stdout: "ok"},
		},
	}, {
		desc:        "manifest not found returns nil",
		nonExistent: true,
		portRange:   "10000-32767",
	}, {
		desc:         "manifest already contains service-node-port-range returns nil",
		manifestData: manifestWithExistingRange,
		portRange:    "10000-32767",
	}, {
		desc:         "could not find insertion point",
		manifestData: manifestNoMatch,
		portRange:    "10000-32767",
		wantErr:      "could not find insertion point",
	}, {
		desc:         "failed to copy modified manifest",
		manifestData: manifestWithClusterIP,
		portRange:    "10000-32767",
		resp: []fexec.Response{
			{Cmd: "sudo", Args: []string{"cp", "-f", ".*", ".*"}, Err: "failed to copy"},
		},
		wantErr: "failed to copy",
	}, {
		desc:         "success after retry",
		manifestData: manifestWithClusterIP,
		portRange:    "10000-32767",
		resp: []fexec.Response{
			{Cmd: "sudo", Args: []string{"cp", "-f", ".*", ".*"}},
			{Cmd: "kubectl", Args: []string{"get", "pod", "-n", "kube-system", "-l", "component=kube-apiserver", "-o", ".*"}, Err: "connection refused"},
			{Cmd: "kubectl", Args: []string{"get", "pod", "-n", "kube-system", "-l", "component=kube-apiserver", "-o", ".*"}, Stdout: "old-args"},
			{Cmd: "kubectl", Args: []string{"get", "pod", "-n", "kube-system", "-l", "component=kube-apiserver", "-o", ".*"}, Stdout: "--service-node-port-range=10000-32767"},
			{Cmd: "kubectl", Args: []string{"get", "--raw", "/readyz"}, Err: "not ready"},
			{Cmd: "kubectl", Args: []string{"get", "pod", "-n", "kube-system", "-l", "component=kube-apiserver", "-o", ".*"}, Stdout: "--service-node-port-range=10000-32767"},
			{Cmd: "kubectl", Args: []string{"get", "--raw", "/readyz"}, Stdout: "ok"},
		},
	}, {
		desc:         "timed out waiting for apiserver",
		manifestData: manifestWithClusterIP,
		portRange:    "10000-32767",
		waitTimeout:  time.Nanosecond,
		resp: []fexec.Response{
			{Cmd: "sudo", Args: []string{"cp", "-f", ".*", ".*"}},
			{Cmd: "kubectl", Args: []string{"get", "pod", "-n", "kube-system", "-l", "component=kube-apiserver", "-o", ".*"}, Err: "connection refused"},
		},
		wantErr: "timed out waiting for kube-apiserver",
	}}

	for _, tt := range tests {
		t.Run(tt.desc, func(t *testing.T) {
			if tt.waitTimeout != 0 {
				apiserverWaitTimeout = tt.waitTimeout
			} else {
				apiserverWaitTimeout = 50 * time.Millisecond
			}
			if tt.nonExistent {
				kubeAPIServerManifest = filepath.Join(t.TempDir(), "nonexistent.yaml")
			} else {
				f, err := os.CreateTemp(t.TempDir(), "kube-apiserver-*.yaml")
				if err != nil {
					t.Fatalf("Failed to create temp file: %v", err)
				}
				if _, err := f.WriteString(tt.manifestData); err != nil {
					t.Fatalf("Failed to write temp manifest: %v", err)
				}
				if err := f.Close(); err != nil {
					t.Fatalf("Failed to close temp manifest: %v", err)
				}
				kubeAPIServerManifest = f.Name()
			}

			fexec.LogCommand = func(s string) {
				t.Logf("%s: %s", tt.desc, s)
			}
			cmds := fexec.Commands(tt.resp)
			kexec.Command = cmds.Command
			defer checkCmds(t, cmds)

			err := SetServiceNodePortRange(tt.portRange)
			if s := errdiff.Substring(err, tt.wantErr); s != "" {
				t.Fatalf("unexpected error: %s", s)
			}
		})
	}
}
