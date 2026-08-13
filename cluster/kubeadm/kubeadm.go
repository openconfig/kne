package kubeadm

import (
	"fmt"
	"os"
	"strings"

	"github.com/openconfig/kne/exec/run"
	log "k8s.io/klog/v2"
)

var (
	kubeadmFlagPath       = "/var/lib/kubelet/kubeadm-flags.env"
	kubeAPIServerManifest = "/etc/kubernetes/manifests/kube-apiserver.yaml"
)

// SetServiceNodePortRange sets the service node port range in the kube-apiserver manifest.
func SetServiceNodePortRange(portRange string) error {
	log.Infof("Setting service node port range to %q...", portRange)
	b, err := os.ReadFile(kubeAPIServerManifest)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		// If read fails (e.g. due to permissions on root-owned manifest), try reading via sudo.
		var sudoErr error
		b, sudoErr = run.OutCommand("sudo", "cat", kubeAPIServerManifest)
		if sudoErr != nil {
			return fmt.Errorf("failed to read %s: %w", kubeAPIServerManifest, err)
		}
	}
	content := string(b)
	if strings.Contains(content, "--service-node-port-range=") {
		return nil
	}
	target := "    - --service-cluster-ip-range="
	idx := strings.Index(content, target)
	if idx == -1 {
		target = "    - kube-apiserver\n"
		idx = strings.Index(content, target)
		if idx == -1 {
			return fmt.Errorf("could not find insertion point in %s", kubeAPIServerManifest)
		}
	}
	endOfLine := strings.Index(content[idx:], "\n")
	if endOfLine == -1 {
		endOfLine = len(content) - idx
	}
	insertPos := idx + endOfLine + 1
	flag := fmt.Sprintf("    - --service-node-port-range=%s\n", portRange)
	newContent := content[:insertPos] + flag + content[insertPos:]

	f, err := os.CreateTemp("", "kne-apiserver-*.yaml")
	if err != nil {
		return err
	}
	defer func() {
		if err := os.Remove(f.Name()); err != nil && !os.IsNotExist(err) {
			log.Warningf("Failed to remove temp file %q: %v", f.Name(), err)
		}
	}()
	if _, err := f.WriteString(newContent); err != nil {
		_ = f.Close()
		return err
	}
	if err := f.Close(); err != nil {
		return err
	}
	if err := run.LogCommand("sudo", "cp", f.Name(), kubeAPIServerManifest); err != nil {
		return err
	}
	return nil
}

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
	defer func() {
		if err := os.Remove(f.Name()); err != nil && !os.IsNotExist(err) {
			log.Warningf("Failed to remove temp file %q: %v", f.Name(), err)
		}
	}()
	if _, err := f.WriteString(s); err != nil {
		_ = f.Close()
		return err
	}
	if err := f.Close(); err != nil {
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
