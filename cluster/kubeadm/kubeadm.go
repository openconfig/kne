package kubeadm

import (
	"bytes"
	"fmt"
	"os"
	"strings"

	"github.com/openconfig/kne/exec/run"
	log "k8s.io/klog/v2"
	"sigs.k8s.io/yaml"
)

var (
	kubeadmFlagPath = "/var/lib/kubelet/kubeadm-flags.env"
)

// InitConfigOptions contains options for generating a kubeadm init configuration file.
type InitConfigOptions struct {
	CRISocket            string
	PodNetworkCIDR       string
	TokenTTL             string
	ImageRepository      string
	ServiceNodePortRange string
}

type initConfiguration struct {
	APIVersion       string            `json:"apiVersion" yaml:"apiVersion"`
	Kind             string            `json:"kind" yaml:"kind"`
	NodeRegistration *nodeRegistration `json:"nodeRegistration,omitempty" yaml:"nodeRegistration,omitempty"`
	BootstrapTokens  []bootstrapToken  `json:"bootstrapTokens,omitempty" yaml:"bootstrapTokens,omitempty"`
}

type nodeRegistration struct {
	CRISocket string `json:"criSocket,omitempty" yaml:"criSocket,omitempty"`
}

type bootstrapToken struct {
	TTL string `json:"ttl,omitempty" yaml:"ttl,omitempty"`
}

type clusterConfiguration struct {
	APIVersion      string            `json:"apiVersion" yaml:"apiVersion"`
	Kind            string            `json:"kind" yaml:"kind"`
	ImageRepository string            `json:"imageRepository,omitempty" yaml:"imageRepository,omitempty"`
	Networking      *networking       `json:"networking,omitempty" yaml:"networking,omitempty"`
	APIServer       *apiServer        `json:"apiServer,omitempty" yaml:"apiServer,omitempty"`
}

type networking struct {
	PodSubnet string `json:"podSubnet,omitempty" yaml:"podSubnet,omitempty"`
}

type apiServer struct {
	ExtraArgs map[string]string `json:"extraArgs,omitempty" yaml:"extraArgs,omitempty"`
}

// CreateInitConfigFile creates a temporary kubeadm init configuration file.
// The caller is responsible for calling the returned cleanup function when done.
func CreateInitConfigFile(opts InitConfigOptions) (string, func(), error) {
	var docs [][]byte

	var initCfg initConfiguration
	if opts.CRISocket != "" {
		initCfg.NodeRegistration = &nodeRegistration{CRISocket: opts.CRISocket}
	}
	if opts.TokenTTL != "" {
		initCfg.BootstrapTokens = []bootstrapToken{{TTL: opts.TokenTTL}}
	}
	if initCfg.NodeRegistration != nil || len(initCfg.BootstrapTokens) > 0 {
		initCfg.APIVersion = "kubeadm.k8s.io/v1beta3"
		initCfg.Kind = "InitConfiguration"
		b, err := yaml.Marshal(initCfg)
		if err != nil {
			return "", nil, fmt.Errorf("failed to marshal InitConfiguration: %w", err)
		}
		docs = append(docs, b)
	}

	clusterCfg := clusterConfiguration{
		APIVersion:      "kubeadm.k8s.io/v1beta3",
		Kind:            "ClusterConfiguration",
		ImageRepository: opts.ImageRepository,
	}
	if opts.PodNetworkCIDR != "" {
		clusterCfg.Networking = &networking{PodSubnet: opts.PodNetworkCIDR}
	}
	portRange := opts.ServiceNodePortRange
	if portRange == "" {
		portRange = "10000-32767"
	}
	clusterCfg.APIServer = &apiServer{
		ExtraArgs: map[string]string{
			"service-node-port-range": portRange,
		},
	}
	b, err := yaml.Marshal(clusterCfg)
	if err != nil {
		return "", nil, fmt.Errorf("failed to marshal ClusterConfiguration: %w", err)
	}
	docs = append(docs, b)

	var buf bytes.Buffer
	for i, doc := range docs {
		if i > 0 {
			buf.WriteString("---\n")
		}
		buf.Write(doc)
	}

	f, err := os.CreateTemp("", "kne-kubeadm-init-*.yaml")
	if err != nil {
		return "", nil, fmt.Errorf("failed to create temp file: %w", err)
	}
	cleanup := func() {
		if err := os.Remove(f.Name()); err != nil && !os.IsNotExist(err) {
			log.Warningf("Failed to remove temp file %q: %v", f.Name(), err)
		}
	}
	if _, err := f.Write(buf.Bytes()); err != nil {
		_ = f.Close()
		cleanup()
		return "", nil, fmt.Errorf("failed to write kubeadm config: %w", err)
	}
	if err := f.Close(); err != nil {
		cleanup()
		return "", nil, fmt.Errorf("failed to close kubeadm config file: %w", err)
	}

	return f.Name(), cleanup, nil
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
	if err := run.LogCommand("sudo", "cp", "-f", f.Name(), kubeadmFlagPath); err != nil {
		return err
	}
	if err := run.LogCommand("sudo", "systemctl", "restart", "kubelet"); err != nil {
		return err
	}
	return nil
}
