package kubeadm

import (
	"fmt"
	"os"
	"strings"

	"github.com/openconfig/kne/exec/run"
)

var (
	kubeadmFlagPath = "/var/lib/kubelet/kubeadm-flags.env"
)

// EnableCredentialProvider enables a credential provider according
// to the specified config file on the kubelet.
func EnableCredentialProvider(cfgPath string) error {
	if _, err := os.Stat(cfgPath); err != nil {
		return fmt.Errorf("config file not found: %v", err)
	}
	if err := run.LogCommand("sudo", "kubeadm", "upgrade", "node", "phase", "kubelet-config"); err != nil {
		return err
	}
	b, err := os.ReadFile(kubeadmFlagPath)
	if err != nil {
		return err
	}
	s, _ := strings.CutSuffix(string(b), `"`)
	s = fmt.Sprintf(`%s --image-credential-provider-config=%s --image-credential-provider-bin-dir=/etc/kubernetes/bin"`, s, cfgPath)
	f, err := os.Create(kubeadmFlagPath)
	if err != nil {
		return err
	}
	if _, err := f.WriteString(s); err != nil {
		return err
	}
	if err := run.LogCommand("sudo", "systemctl", "restart", "kubelet"); err != nil {
		return err
	}
	return nil
}
