package kubeadm

import (
	"fmt"
	"os"
	"strings"

	"github.com/openconfig/kne/exec/run"
	log "k8s.io/klog/v2"
)

var (
	kubeadmFlagPath = "/var/lib/kubelet/kubeadm-flags.env"
)

// EnableCredentialProvider enables a credential provider according
// to the specified config file on the kubelet.
func EnableCredentialProvider(cfgPath string) error {
	log.Infof("Enabling credential provider with config %q...", cfgPath)
	if _, err := os.Stat(cfgPath); err != nil {
		return fmt.Errorf("config file not found: %v", err)
	}
	if err := run.LogCommand("sudo", "kubeadm", "upgrade", "node", "phase", "kubelet-config"); err != nil {
		return err
	}
	b, err := os.ReadFile(kubeadmFlagPath)
	if err != nil {
		return fmt.Errorf("failed to read kubeadm flag file: %v", err)
	}
	s, ok := strings.CutSuffix(string(b), "\"\n")
	if !ok {
		return fmt.Errorf("kubeadm flag file %q does not have expected contents: %q", kubeadmFlagPath, s)
	}
	s = fmt.Sprintf("%s --image-credential-provider-config=%s --image-credential-provider-bin-dir=/etc/kubernetes/bin\"\n", s, cfgPath)
	f, err := os.CreateTemp("", "kne-kubeadm-flag.env")
	if err != nil {
		return err
	}
	defer os.RemoveAll(f.Name())
	if _, err := f.WriteString(s); err != nil {
		return err
	}
	if err := run.LogCommand("sudo", "cp", f.Name(), kubeadmFlagPath); err != nil {
		return err
	}
	if err := run.LogCommand("sudo", "systemctl", "restart", "kubelet"); err != nil {
		return err
	}
	return nil
}
