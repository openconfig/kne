package deploy

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/blang/semver"
	dtypes "github.com/docker/docker/api/types"
	dclient "github.com/docker/docker/client"
	"github.com/openconfig/gnmi/errlist"
	metallbclientv1 "github.com/openconfig/kne/api/metallb/clientset/v1beta1"
	"github.com/openconfig/kne/cluster/kind"
	"github.com/openconfig/kne/cluster/kubeadm"
	"github.com/openconfig/kne/events"
	"github.com/openconfig/kne/exec/run"
	"github.com/openconfig/kne/load"
	"github.com/openconfig/kne/metrics"
	"github.com/openconfig/kne/pods"
	epb "github.com/openconfig/kne/proto/event"
	"github.com/pborman/uuid"
	metallbv1 "go.universe.tf/metallb/api/v1beta1"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/fields"
	kversion "k8s.io/apimachinery/pkg/version"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"
	"k8s.io/client-go/util/homedir"
	log "k8s.io/klog/v2"
	"sigs.k8s.io/yaml"
)

var (
	setPIDMaxScript = filepath.Join(homedir.HomeDir(), "kne-internal", "set_pid_max.sh")
	pullRetryDelay  = time.Second
	poolRetryDelay  = 5 * time.Second
	healthTimeout   = time.Minute
	criDocker       = "cri-docker"

	// Stubs for testing.
	execLookPath       = exec.LookPath
	kindSetupGARAccess = kind.SetupGARAccess
	homeDir            = homedir.HomeDir
)

type Cluster interface {
	Deploy(context.Context) error
	Delete() error
	Healthy() error
	GetName() string
	GetDockerNetworkResourceName() string
	Apply([]byte) error
}

type Ingress interface {
	Deploy(context.Context) error
	SetKClient(kubernetes.Interface)
	Healthy(context.Context) error
	SetRCfg(*rest.Config)
	SetDockerNetworkResourceName(string)
}

type CNI interface {
	Deploy(context.Context) error
	SetKClient(kubernetes.Interface)
	Healthy(context.Context) error
}

type Controller interface {
	Deploy(context.Context) error
	SetKClient(kubernetes.Interface)
	Healthy(context.Context) error
}

type Deployment struct {
	Cluster     Cluster      `kne:"cluster"`
	Ingress     Ingress      `kne:"ingress"`
	CNI         CNI          `kne:"cni"`
	Controllers []Controller `kne:"controllers"`

	// If Progress is true then deployment status updates will be sent to
	// standard output.
	Progress bool

	// If ReportUsage is true then anonymous usage metrics will be
	// published using Cloud PubSub.
	ReportUsage bool
	// ReportUsageProjectID is the ID of the GCP project the usage
	// metrics should be written to. This field is not used if
	// ReportUsage is unset. An empty string will result in the
	// default project being used.
	ReportUsageProjectID string
	// ReportUsageTopicID is the ID of the GCP PubSub topic the usage
	// metrics should be written to. This field is not used if
	// ReportUsage is unset. An empty string will result in the
	// default topic being used.
	ReportUsageTopicID string
}

func (d *Deployment) String() string {
	b, _ := json.MarshalIndent(d, "", "\t")
	return string(b)
}

func (d *Deployment) checkDependencies() error {
	var errs errlist.List
	for _, bin := range []string{"docker", "kubectl"} {
		if _, err := execLookPath(bin); err != nil {
			errs.Add(fmt.Errorf("install dependency %q to deploy", bin))
		}
	}
	return errs.Err()
}

type kubeVersion struct {
	ClientVersion    *kversion.Info `json:"clientVersion,omitempty" yaml:"clientVersion,omitempty"`
	KustomizeVersion string         `json:"kustomizeVersion,omitempty" yaml:"kustomizeVersion,omitempty"`
	ServerVersion    *kversion.Info `json:"serverVersion,omitempty" yaml:"serverVersion,omitempty"`
}

// event turns the deployment into a cluster event protobuf.
func (d *Deployment) event() *epb.Cluster {
	c := &epb.Cluster{}
	switch d.Cluster.(type) {
	case *ExternalSpec:
		c.Cluster = epb.Cluster_CLUSTER_TYPE_EXTERNAL
	case *KindSpec:
		c.Cluster = epb.Cluster_CLUSTER_TYPE_KIND
	case *KubeadmSpec:
		c.Cluster = epb.Cluster_CLUSTER_TYPE_KUBEADM
	}
	switch d.Ingress.(type) {
	case *MetalLBSpec:
		c.Ingress = epb.Cluster_INGRESS_TYPE_METALLB
	}
	switch d.CNI.(type) {
	case *MeshnetSpec:
		c.Cni = epb.Cluster_CNI_TYPE_MESHNET
	}
	for _, cntrl := range d.Controllers {
		switch cntrl.(type) {
		case *CEOSLabSpec:
			c.Controllers = append(c.Controllers, epb.Cluster_CONTROLLER_TYPE_CEOSLAB)
		case *IxiaTGSpec:
			c.Controllers = append(c.Controllers, epb.Cluster_CONTROLLER_TYPE_IXIATG)
		case *SRLinuxSpec:
			c.Controllers = append(c.Controllers, epb.Cluster_CONTROLLER_TYPE_SRLINUX)
		case *LemmingSpec:
			c.Controllers = append(c.Controllers, epb.Cluster_CONTROLLER_TYPE_LEMMING)
		case *CdnosSpec:
			c.Controllers = append(c.Controllers, epb.Cluster_CONTROLLER_TYPE_CDNOS)
		}
	}
	return c
}

func (d *Deployment) reportDeployEvent(ctx context.Context) func(error) {
	r, err := metrics.NewReporter(ctx, d.ReportUsageProjectID, d.ReportUsageTopicID)
	if err != nil {
		log.Warningf("Unable to create metrics reporter: %v", err)
		return func(_ error) {}
	}
	id, err := r.ReportDeployClusterStart(ctx, d.event())
	if err != nil {
		log.Warningf("Unable to report cluster deployment start event: %v", err)
		return func(_ error) { r.Close() }
	}
	return func(rerr error) {
		defer r.Close()
		if err := r.ReportDeployClusterEnd(ctx, id, rerr); err != nil {
			log.Warningf("Unable to report cluster deployment end event: %v", err)
		}
	}
}

func (d *Deployment) Deploy(ctx context.Context, kubecfg string) (rerr error) {
	if d.ReportUsage {
		finish := d.reportDeployEvent(ctx)
		defer func() { finish(rerr) }()
	}
	if err := d.checkDependencies(); err != nil {
		return fmt.Errorf("failed to check for dependencies: %w", err)
	}
	log.Infof("Deploying cluster...")
	if err := d.Cluster.Deploy(ctx); err != nil {
		return fmt.Errorf("failed to deploy cluster: %w", err)
	}
	log.Infof("Cluster deployed")
	if err := d.Cluster.Healthy(); err != nil {
		return fmt.Errorf("failed to check if cluster is healthy: %w", err)
	}
	log.Infof("Cluster healthy")
	// Once cluster is up, set kClient
	rCfg, err := clientcmd.BuildConfigFromFlags("", kubecfg)
	if err != nil {
		return fmt.Errorf("failed to create k8s config: %w", err)
	}
	kClient, err := kubernetes.NewForConfig(rCfg)
	if err != nil {
		return fmt.Errorf("failed to create k8s client: %w", err)
	}

	log.Infof("Validating kubectl version")
	if err := validateKubectlVersion(); err != nil {
		return fmt.Errorf("kubectl version outside of supported range: %v", err)
	}

	ctx, cancel := context.WithCancel(ctx)

	// Watch the containter status of the pods so we can fail if a container fails to start running.
	if w, err := pods.NewWatcher(ctx, kClient, cancel); err != nil {
		log.Warningf("Failed to start pod watcher: %v", err)
	} else {
		w.SetProgress(d.Progress)
		defer func() {
			cancel()
			rerr = w.Cleanup(rerr)
		}()
	}
	// Watch for incoming events to fail early in case of events signaling unrecoverable errors.
	if w, err := events.NewWatcher(ctx, kClient, cancel); err != nil {
		log.Warningf("Failed to start event watcher: %v", err)
	} else {
		w.SetProgress(d.Progress)
		defer func() {
			cancel()
			rerr = w.Cleanup(rerr)
		}()
	}

	d.Ingress.SetKClient(kClient)
	d.Ingress.SetRCfg(rCfg)
	d.Ingress.SetDockerNetworkResourceName(d.Cluster.GetDockerNetworkResourceName())

	log.Infof("Deploying ingress...")
	if err := d.Ingress.Deploy(ctx); err != nil {
		return fmt.Errorf("failed to deploy ingress: %w", err)
	}
	tCtx, cancel := context.WithTimeout(ctx, healthTimeout)
	defer cancel()
	if err := d.Ingress.Healthy(tCtx); err != nil {
		return fmt.Errorf("failed to check if ingress is healthy: %w", err)
	}
	log.Infof("Ingress healthy")
	log.Infof("Deploying CNI...")
	if err := d.CNI.Deploy(ctx); err != nil {
		return fmt.Errorf("failed to deploy CNI: %w", err)
	}
	d.CNI.SetKClient(kClient)
	tCtx, cancel = context.WithTimeout(ctx, healthTimeout)
	defer cancel()
	if err := d.CNI.Healthy(tCtx); err != nil {
		return fmt.Errorf("failed to check if CNI is healthy: %w", err)
	}
	log.Infof("CNI healthy")
	for _, c := range d.Controllers {
		log.Infof("Deploying controller...")
		if err := c.Deploy(ctx); err != nil {
			return fmt.Errorf("failed to deploy controller: %w", err)
		}
		c.SetKClient(kClient)
		tCtx, cancel = context.WithTimeout(ctx, healthTimeout)
		defer cancel()
		if err := c.Healthy(tCtx); err != nil {
			return fmt.Errorf("failed to check if controller is healthy: %w", err)
		}
	}
	log.Infof("Controllers deployed and healthy")
	return nil
}

func validateKubectlVersion() error {
	output, err := run.OutCommand("kubectl", "version", "--output=yaml")
	if err != nil {
		return fmt.Errorf("failed get kubectl version: %w", err)
	}
	log.V(1).Info("Found k8s versions:\n", string(output))
	kubeYAML := kubeVersion{}
	if err := yaml.Unmarshal(output, &kubeYAML); err != nil {
		return fmt.Errorf("failed get kubectl version: %w", err)
	}
	kClientVersion, err := parseVersion(kubeYAML.ClientVersion.GitVersion)
	if err != nil {
		return fmt.Errorf("failed to parse k8s client version: %w", err)
	}
	kServerVersion, err := parseVersion(kubeYAML.ServerVersion.GitVersion)
	if err != nil {
		return fmt.Errorf("failed to parse k8s server version: %w", err)
	}
	origMajor := kClientVersion.Major
	if kClientVersion.Major < 2 {
		kClientVersion.Major = 0
	} else {
		kClientVersion.Major -= 2
	}
	if kServerVersion.LT(kClientVersion) {
		log.Warning("Kube client and server versions are not within expected range.")
	}
	kClientVersion.Major = origMajor + 2
	if kClientVersion.LT(kServerVersion) {
		log.Warning("Kube client and server versions are not within expected range.")
	}
	return nil
}

// parseVersion takes a github semver string and parses it into a comparable struct
// with prereleases and builds stripped.
func parseVersion(s string) (semver.Version, error) {
	if !strings.HasPrefix(s, "v") {
		return semver.Version{}, fmt.Errorf("missing prefix on major version")
	}
	v, err := semver.Parse(s[1:])
	if err != nil {
		return semver.Version{}, err
	}
	v.Pre = nil
	v.Build = nil
	return v, nil
}

func (d *Deployment) Delete() error {
	log.Infof("Deleting cluster...")
	if err := d.Cluster.Delete(); err != nil {
		return fmt.Errorf("failed to delete cluster: %w", err)
	}
	log.Infof("Cluster deleted")
	return nil
}

func (d *Deployment) Healthy(ctx context.Context) error {
	if err := d.Cluster.Healthy(); err != nil {
		return fmt.Errorf("failed to check cluster is healthy: %w", err)
	}
	log.Infof("Cluster healthy")
	tCtx, cancel := context.WithTimeout(ctx, healthTimeout)
	defer cancel()
	if err := d.Ingress.Healthy(tCtx); err != nil {
		return fmt.Errorf("failed to check ingress is healthy: %w", err)
	}
	log.Infof("Ingress healthy")
	tCtx, cancel = context.WithTimeout(ctx, healthTimeout)
	defer cancel()
	if err := d.CNI.Healthy(tCtx); err != nil {
		return fmt.Errorf("failed to check CNI is healthy: %w", err)
	}
	log.Infof("CNI healthy")
	for _, c := range d.Controllers {
		tCtx, cancel = context.WithTimeout(ctx, healthTimeout)
		defer cancel()
		if err := c.Healthy(tCtx); err != nil {
			return fmt.Errorf("failed to check controller is healthy: %w", err)
		}
	}
	log.Infof("Controllers healthy")
	return nil
}

func init() {
	load.Register("External", &load.Spec{
		Type: ExternalSpec{},
		Tag:  "cluster",
	})
}

type ExternalSpec struct {
	Network string `yaml:"network"`
}

func (e *ExternalSpec) Deploy(ctx context.Context) error {
	log.Infof("Deploy is a no-op for the external cluster type")
	return nil
}

func (e *ExternalSpec) Delete() error {
	log.Infof("Delete is a no-op for the external cluster type")
	return nil
}

func (e *ExternalSpec) Healthy() error {
	if err := run.LogCommand("kubectl", "cluster-info"); err != nil {
		return fmt.Errorf("cluster not healthy: %w", err)
	}
	return nil
}

func (e *ExternalSpec) GetName() string {
	return "kne"
}

func (e *ExternalSpec) GetDockerNetworkResourceName() string {
	return e.Network
}

func (e *ExternalSpec) Apply(cfg []byte) error {
	return kubectlApply(cfg)
}

func kubectlApply(cfg []byte) error {
	return run.LogCommandWithInput(cfg, "kubectl", "apply", "-f", "-")
}

func init() {
	load.Register("Kubeadm", &load.Spec{
		Type: KubeadmSpec{},
		Tag:  "cluster",
	})
}

type KubeadmSpec struct {
	CRISocket                   string `yaml:"criSocket"`
	PodNetworkCIDR              string `yaml:"podNetworkCIDR"`
	PodNetworkAddOnManifest     string `yaml:"podNetworkAddOnManifest" kne:"yaml"`
	PodNetworkAddOnManifestData []byte
	CredentialProviderConfig    string `yaml:"credentialProviderConfig" kne:"yaml"`
	TokenTTL                    string `yaml:"tokenTTL"`
	Network                     string `yaml:"network"`
	AllowControlPlaneScheduling bool   `yaml:"allowControlPlaneScheduling"`
}

func (k *KubeadmSpec) checkDependencies() error {
	var errs errlist.List
	bins := []string{"kubeadm"}
	for _, bin := range bins {
		if _, err := execLookPath(bin); err != nil {
			errs.Add(fmt.Errorf("install dependency %q to deploy", bin))
		}
	}
	return errs.Err()
}

func (k *KubeadmSpec) Deploy(ctx context.Context) error {
	if err := k.checkDependencies(); err != nil {
		return fmt.Errorf("failed to check for dependencies: %w", err)
	}
	args := []string{"kubeadm", "init"}
	if k.CRISocket != "" {
		args = append(args, "--cri-socket", k.CRISocket)
		// If using cri-docker, then ensure the components are running.
		if strings.Contains(k.CRISocket, criDocker) {
			if err := run.LogCommand("sudo", "systemctl", "enable", "--now", criDocker+".socket"); err != nil {
				return err
			}
			if err := run.LogCommand("sudo", "systemctl", "enable", "--now", criDocker+".service"); err != nil {
				return err
			}
		}
	}
	if k.PodNetworkCIDR != "" {
		args = append(args, "--pod-network-cidr", k.PodNetworkCIDR)
	}
	if k.TokenTTL != "" {
		args = append(args, "--token-ttl", k.TokenTTL)
	}
	if err := run.LogCommand("sudo", args...); err != nil {
		return err
	}
	kubeDir := filepath.Join(homeDir(), ".kube")
	if err := os.MkdirAll(kubeDir, 0750); err != nil {
		return err
	}
	b, err := run.OutCommand("sudo", "cat", "/etc/kubernetes/admin.conf")
	if err != nil {
		return err
	}
	if err := os.WriteFile(filepath.Join(kubeDir, "config"), b, 0600); err != nil {
		return err
	}
	if k.AllowControlPlaneScheduling {
		if err := run.LogCommand("kubectl", "taint", "nodes", "--all", "node-role.kubernetes.io/control-plane:NoSchedule-"); err != nil {
			return err
		}
	}
	// If add on is provided, apply it.
	if k.PodNetworkAddOnManifestData != nil {
		f, err := os.CreateTemp("", "kubeadm-pod-network-add-on-manifest-*.yaml")
		if err != nil {
			return err
		}
		defer os.Remove(f.Name())
		if _, err := f.Write(k.PodNetworkAddOnManifestData); err != nil {
			return err
		}
		if err := f.Close(); err != nil {
			return err
		}
		k.PodNetworkAddOnManifest = f.Name()
	}
	if k.PodNetworkAddOnManifest != "" {
		b, err := os.ReadFile(k.PodNetworkAddOnManifest)
		if err != nil {
			return err
		}
		if err := kubectlApply(b); err != nil {
			return err
		}
	}
	// If credential provider config provided, apply it.
	if k.CredentialProviderConfig != "" {
		if err := kubeadm.EnableCredentialProvider(k.CredentialProviderConfig); err != nil {
			return err
		}
	}
	// Create a new docker network if not specified.
	if k.Network == "" {
		k.Network = "kne-kubeadm-" + uuid.New()
		if err := run.LogCommand("docker", "network", "create", k.Network); err != nil {
			return err
		}
	}
	return nil
}

func (k *KubeadmSpec) Delete() error {
	args := []string{"kubeadm", "reset", "--force"}
	if k.CRISocket != "" {
		args = append(args, "--cri-socket", k.CRISocket)
	}
	if err := run.LogCommand("sudo", args...); err != nil {
		return err
	}
	return nil
}

func (k *KubeadmSpec) Healthy() error {
	if err := run.LogCommand("kubectl", "cluster-info"); err != nil {
		return fmt.Errorf("cluster not healthy: %w", err)
	}
	return nil
}

func (k *KubeadmSpec) GetName() string {
	return "kne"
}

func (k *KubeadmSpec) GetDockerNetworkResourceName() string {
	return k.Network
}

func (k *KubeadmSpec) Apply(cfg []byte) error {
	return kubectlApply(cfg)
}

func init() {
	load.Register("Kind", &load.Spec{
		Type: KindSpec{},
		Tag:  "cluster",
	})
}

type KindSpec struct {
	Name                     string            `yaml:"name"`
	Recycle                  bool              `yaml:"recycle"`
	Version                  string            `yaml:"version"`
	Image                    string            `yaml:"image"`
	Retain                   bool              `yaml:"retain"`
	Wait                     time.Duration     `yaml:"wait"`
	Kubecfg                  string            `yaml:"kubecfg" kne:"yaml"`
	GoogleArtifactRegistries []string          `yaml:"googleArtifactRegistries"`
	ContainerImages          map[string]string `yaml:"containerImages"`
	KindConfigFile           string            `yaml:"config" kne:"yaml"`
	AdditionalManifests      []string          `yaml:"additionalManifests" kne:"yaml"`
}

func (k *KindSpec) checkDependencies() error {
	var errs errlist.List
	bins := []string{"kind"}
	for _, bin := range bins {
		if _, err := execLookPath(bin); err != nil {
			errs.Add(fmt.Errorf("install dependency %q to deploy", bin))
		}
	}
	if errs.Err() != nil {
		return errs.Err()
	}
	if k.Version != "" {
		wantV, err := parseVersion(k.Version)
		if err != nil {
			return fmt.Errorf("failed to parse desired kind version: %w", err)
		}

		stdout, err := run.OutCommand("kind", "version")
		if err != nil {
			return fmt.Errorf("failed to get kind version: %w", err)
		}

		vKindFields := strings.Fields(string(stdout))
		if len(vKindFields) < 2 {
			return fmt.Errorf("failed to parse kind version from: %s", stdout)
		}

		gotV, err := parseVersion(vKindFields[1])
		if err != nil {
			return fmt.Errorf("kind version check failed: %w", err)
		}
		if gotV.LT(wantV) {
			return fmt.Errorf("kind version check failed: got %s, want %s. install with `go install sigs.k8s.io/kind@%s`", gotV, wantV, wantV)
		}
		log.Infof("kind version valid: got %s want %s", gotV, wantV)
	}
	return nil
}

func (k *KindSpec) create() error {
	// Create a KNE dir under /tmp intended to hold files to be mounted into the kind cluster.
	if err := os.MkdirAll("/tmp/kne", os.ModePerm); err != nil {
		return err
	}
	if k.Recycle {
		log.Infof("Attempting to recycle existing cluster %q...", k.Name)
		if err := run.LogCommand("kubectl", "cluster-info", "--context", fmt.Sprintf("kind-%s", k.Name)); err == nil {
			log.Infof("Recycling existing cluster %q", k.Name)
			return nil
		}
	}
	args := []string{"create", "cluster"}
	if k.Name != "" {
		args = append(args, "--name", k.Name)
	}
	if k.Image != "" {
		args = append(args, "--image", k.Image)
	}
	if k.Retain {
		args = append(args, "--retain")
	}
	if k.Wait != 0 {
		args = append(args, "--wait", k.Wait.String())
	}
	if k.Kubecfg != "" {
		args = append(args, "--kubeconfig", k.Kubecfg)
	}
	if k.KindConfigFile != "" {
		args = append(args, "--config", k.KindConfigFile)
	}
	log.Infof("Creating kind cluster with: %v", args)
	if out, err := run.OutLogCommand("kind", args...); err != nil {
		msg := []string{}
		// Filter output to only show lines relevant to the error message. For kind these are lines
		// prefixed with "ERROR" or "Command Output".
		for _, line := range strings.Split(string(out), "\n") {
			if strings.HasPrefix(line, "ERROR") || strings.HasPrefix(line, "Command Output") {
				msg = append(msg, line)
			}
		}
		return fmt.Errorf("%w: %v", err, strings.Join(msg, ", "))
	}
	log.Infof("Deployed kind cluster: %s", k.Name)
	return nil
}

func (k *KindSpec) Deploy(ctx context.Context) error {
	if err := k.checkDependencies(); err != nil {
		return fmt.Errorf("failed to check for dependencies: %w", err)
	}

	if err := k.create(); err != nil {
		return fmt.Errorf("failed to create kind cluster: %w", err)
	}

	// If the script is found, then run it. Else silently ignore it.
	// The set_pid_max script modifies the kernel.pid_max value to
	// be acceptable for the Cisco 8000e container.
	if _, err := os.Stat(setPIDMaxScript); err == nil {
		if err := run.LogCommand(setPIDMaxScript); err != nil {
			return fmt.Errorf("failed to exec set_pid_max script: %w", err)
		}
	}

	for _, s := range k.AdditionalManifests {
		log.Infof("Found manifest %q", s)
		if err := run.LogCommand("kubectl", "apply", "-f", s); err != nil {
			return fmt.Errorf("failed to deploy manifest: %w", err)
		}
	}

	if len(k.GoogleArtifactRegistries) != 0 {
		log.Infof("Setting up GAR access for %v", k.GoogleArtifactRegistries)
		if err := k.setupGoogleArtifactRegistryAccess(ctx); err != nil {
			return fmt.Errorf("failed to setup GAR access: %w", err)
		}
	}

	if len(k.ContainerImages) != 0 {
		log.Infof("Loading container images")
		if err := k.loadContainerImages(); err != nil {
			return fmt.Errorf("failed to load container images: %w", err)
		}
	}

	return nil
}

func (k *KindSpec) Delete() error {
	args := []string{"delete", "cluster"}
	if k.Name != "" {
		args = append(args, "--name", k.Name)
	}
	if err := run.LogCommand("kind", args...); err != nil {
		return fmt.Errorf("failed to delete cluster: %w", err)
	}
	return nil
}

func (k *KindSpec) Healthy() error {
	if err := run.LogCommand("kubectl", "cluster-info", "--context", fmt.Sprintf("kind-%s", k.GetName())); err != nil {
		return fmt.Errorf("cluster not healthy: %w", err)
	}
	return nil
}

func (k *KindSpec) GetName() string {
	if k.Name != "" {
		return k.Name
	}
	return "kne"
}

func (k *KindSpec) GetDockerNetworkResourceName() string {
	return "kind"
}

func (k *KindSpec) Apply(cfg []byte) error {
	return kubectlApply(cfg)
}

func (k *KindSpec) setupGoogleArtifactRegistryAccess(ctx context.Context) error {
	return kindSetupGARAccess(ctx, k.GoogleArtifactRegistries)
}

func (k *KindSpec) loadContainerImages() error {
	for s, d := range k.ContainerImages {
		if s == "" {
			return fmt.Errorf("source container must not be empty")
		}
		if d == "" {
			log.Infof("Loading %q", s)
			d = s
		} else {
			log.Infof("Loading %q as %q", s, d)
		}
		retries := 3
		var out []byte
		var err error
		for ; ; retries-- {
			out, err = run.OutCommand("docker", "pull", s)
			// Command succeeded or out of retries then break.
			if err == nil || retries == 0 {
				break
			}
			// If container is not found or does not exist, the error is considered not retriable.
			if err != nil && (strings.Contains(string(out), "not found") || strings.Contains(string(out), "does not exist")) {
				err = fmt.Errorf("container not found: %w", err)
				break
			}
			log.Warningf("Failed to pull %q: %w (will retry %d times)", s, err, retries)
			time.Sleep(pullRetryDelay)
		}
		if err != nil {
			return err
		}
		if d != s {
			if err := run.LogCommand("docker", "tag", s, d); err != nil {
				return fmt.Errorf("failed to tag %q with %q: %w", s, d, err)
			}
		}
		args := []string{"load", "docker-image", d}
		if k.Name != "" {
			args = append(args, "--name", k.Name)
		}
		if err := run.LogCommand("kind", args...); err != nil {
			return fmt.Errorf("failed to load %q: %w", d, err)
		}
	}
	log.Infof("Loaded all container images")
	return nil
}

func init() {
	load.Register("MetalLB", &load.Spec{
		Type: MetalLBSpec{},
		Tag:  "ingress",
	})
}

type MetalLBSpec struct {
	IPCount                   int    `yaml:"ip_count"`
	ManifestDir               string `yaml:"manifests"`
	Manifest                  string `yaml:"manifest" kne:"yaml"`
	ManifestData              []byte
	dockerNetworkResourceName string
	kClient                   kubernetes.Interface
	mClient                   metallbclientv1.Interface
	rCfg                      *rest.Config
	dClient                   dclient.NetworkAPIClient
}

func (m *MetalLBSpec) SetKClient(c kubernetes.Interface) {
	m.kClient = c
}

func (m *MetalLBSpec) SetRCfg(cfg *rest.Config) {
	m.rCfg = cfg
}

func (m *MetalLBSpec) SetDockerNetworkResourceName(name string) {
	m.dockerNetworkResourceName = name
}

func inc(ip net.IP, cnt int) {
	for cnt > 0 {
		for j := len(ip) - 1; j >= 0; j-- {
			ip[j]++
			if ip[j] > 0 {
				break
			}
		}
		cnt--
	}
}

func makePool(n *net.IPNet, count int) *metallbv1.IPAddressPool {
	start := make(net.IP, len(n.IP))
	copy(start, n.IP)
	inc(start, 50)
	end := make(net.IP, len(start))
	copy(end, start)
	inc(end, count)
	return &metallbv1.IPAddressPool{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: "metallb-system",
			Name:      "kne-service-pool",
		},
		Spec: metallbv1.IPAddressPoolSpec{
			Addresses: []string{fmt.Sprintf("%s - %s", start, end)},
		},
	}
}

func (m *MetalLBSpec) Deploy(ctx context.Context) error {
	var err error
	if m.dClient == nil {
		m.dClient, err = dclient.NewClientWithOpts(dclient.FromEnv, dclient.WithAPIVersionNegotiation())
		if err != nil {
			return fmt.Errorf("failed to create docker client: %w", err)
		}
	}
	if m.mClient == nil {
		m.mClient, err = metallbclientv1.NewForConfig(m.rCfg)
		if err != nil {
			return fmt.Errorf("failed to create metallb client: %w", err)
		}
	}

	log.Infof("Creating metallb namespace")
	if m.ManifestData != nil {
		f, err := os.CreateTemp("", "metallb-manifest-*.yaml")
		if err != nil {
			return err
		}
		defer os.Remove(f.Name())
		if _, err := f.Write(m.ManifestData); err != nil {
			return err
		}
		if err := f.Close(); err != nil {
			return err
		}
		m.Manifest = f.Name()
	}
	if m.Manifest == "" && m.ManifestDir != "" {
		log.Errorf("Deploying MetalLB using the directory 'manifests' field (%v) is deprecated, instead provide the filepath of the manifest file directly using the 'manifest' field going forward", m.ManifestDir)
		m.Manifest = filepath.Join(m.ManifestDir, "metallb-native.yaml")
	}
	log.Infof("Deploying MetalLB from: %s", m.Manifest)
	if err := run.LogCommand("kubectl", "apply", "-f", m.Manifest); err != nil {
		return fmt.Errorf("failed to deploy metallb: %w", err)
	}
	if _, err := m.kClient.CoreV1().Secrets("metallb-system").Get(ctx, "memberlist", metav1.GetOptions{}); err != nil {
		log.Infof("Creating metallb secret")
		d := make([]byte, 16)
		if _, err := rand.Read(d); err != nil {
			return err
		}
		s := &corev1.Secret{
			ObjectMeta: metav1.ObjectMeta{
				Name: "memberlist",
			},
			StringData: map[string]string{
				"secretkey": base64.StdEncoding.EncodeToString(d),
			},
		}
		if _, err := m.kClient.CoreV1().Secrets("metallb-system").Create(ctx, s, metav1.CreateOptions{}); err != nil {
			return fmt.Errorf("failed to create metallb secret: %w", err)
		}
	}

	// Wait for metallb to be healthy
	if err := m.Healthy(ctx); err != nil {
		return fmt.Errorf("metallb not healthy: %w", err)
	}

	if _, err = m.mClient.IPAddressPool("metallb-system").Get(ctx, "kne-service-pool", metav1.GetOptions{}); err != nil {
		log.Infof("Applying metallb ingress config")
		// Get Network information from docker.
		nr, err := m.dClient.NetworkList(ctx, dtypes.NetworkListOptions{})
		if err != nil {
			return fmt.Errorf("failed to get docker network list: %w", err)
		}
		var network dtypes.NetworkResource
		for _, v := range nr {
			name := m.dockerNetworkResourceName
			if name == "" {
				name = "bridge"
			}
			if v.Name == name {
				network = v
				break
			}
		}
		var n *net.IPNet
		for _, ipRange := range network.IPAM.Config {
			_, ipNet, err := net.ParseCIDR(ipRange.Subnet)
			if err != nil {
				return fmt.Errorf("failed to parse cidr: %w", err)
			}
			if ipNet.IP.To4() != nil {
				n = ipNet
				break
			}
		}
		if n == nil {
			return fmt.Errorf("failed to find kind ipv4 docker net")
		}
		pool := makePool(n, m.IPCount)
		retries := 5
		for ; ; retries-- {
			_, err = m.mClient.IPAddressPool("metallb-system").Create(ctx, pool, metav1.CreateOptions{})
			if err == nil || retries == 0 {
				break
			}
			log.Warningf("Failed to create address polling (will retry %d times)", retries)
			time.Sleep(poolRetryDelay)
		}
		if err != nil {
			return err
		}
		l2Advert := &metallbv1.L2Advertisement{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "kne-l2-service-pool",
				Namespace: "metallb-system",
			},
			Spec: metallbv1.L2AdvertisementSpec{
				IPAddressPools: []string{"kne-service-pool"},
			},
		}
		if _, err = m.mClient.L2Advertisement("metallb-system").Create(ctx, l2Advert, metav1.CreateOptions{}); err != nil {
			return fmt.Errorf("failed to create metallb L2 advertisement: %w", err)
		}
	}
	return nil
}

func (m *MetalLBSpec) Healthy(ctx context.Context) error {
	return deploymentHealthy(ctx, m.kClient, "metallb-system")
}

func init() {
	load.Register("Meshnet", &load.Spec{
		Type: MeshnetSpec{},
		Tag:  "cni",
	})
}

type MeshnetSpec struct {
	ManifestDir  string `yaml:"manifests"`
	Manifest     string `yaml:"manifest" kne:"yaml"`
	ManifestData []byte
	kClient      kubernetes.Interface
}

func (m *MeshnetSpec) SetKClient(c kubernetes.Interface) {
	m.kClient = c
}

func (m *MeshnetSpec) Deploy(ctx context.Context) error {
	if m.ManifestData != nil {
		f, err := os.CreateTemp("", "meshnet-manifest-*.yaml")
		if err != nil {
			return err
		}
		defer os.Remove(f.Name())
		if _, err := f.Write(m.ManifestData); err != nil {
			return err
		}
		if err := f.Close(); err != nil {
			return err
		}
		m.Manifest = f.Name()
	}
	if m.Manifest == "" && m.ManifestDir != "" {
		log.Errorf("Deploying Meshnet using the directory 'manifests' field (%v) is deprecated, instead provide the filepath of the manifest file directly using the 'manifest' field going forward", m.ManifestDir)
		m.Manifest = filepath.Join(m.ManifestDir, "manifest.yaml")
	}
	log.Infof("Deploying Meshnet from: %s", m.Manifest)
	if err := run.LogCommand("kubectl", "apply", "-f", m.Manifest); err != nil {
		return fmt.Errorf("failed to deploy meshnet: %w", err)
	}
	log.Infof("Meshnet Deployed")
	return nil
}

func (m *MeshnetSpec) Healthy(ctx context.Context) error {
	log.Infof("Waiting on Meshnet to be Healthy")
	w, err := m.kClient.AppsV1().DaemonSets("meshnet").Watch(ctx, metav1.ListOptions{
		FieldSelector: fields.SelectorFromSet(fields.Set{metav1.ObjectNameField: "meshnet"}).String(),
	})
	if err != nil {
		return err
	}
	for {
		select {
		case <-ctx.Done():
			return fmt.Errorf("context canceled before meshnet healthy")
		case e, ok := <-w.ResultChan():
			if !ok {
				return fmt.Errorf("watch channel closed before meshnet healthy")
			}
			d, ok := e.Object.(*appsv1.DaemonSet)
			if !ok {
				return fmt.Errorf("invalid object type: %T", d)
			}
			if d.Status.NumberReady == d.Status.DesiredNumberScheduled &&
				d.Status.NumberUnavailable == 0 {
				log.Infof("Meshnet Healthy")
				return nil
			}
		}
	}
}

func init() {
	load.Register("CEOSLab", &load.Spec{
		Type: CEOSLabSpec{},
		Tag:  "controllers",
	})
}

type CEOSLabSpec struct {
	ManifestDir  string `yaml:"manifests"`
	Operator     string `yaml:"operator" kne:"yaml"`
	OperatorData []byte
	kClient      kubernetes.Interface
}

func (c *CEOSLabSpec) SetKClient(k kubernetes.Interface) {
	c.kClient = k
}

func (c *CEOSLabSpec) Deploy(ctx context.Context) error {
	if c.OperatorData != nil {
		f, err := os.CreateTemp("", "ceoslab-operator-*.yaml")
		if err != nil {
			return err
		}
		defer os.Remove(f.Name())
		if _, err := f.Write(c.OperatorData); err != nil {
			return err
		}
		if err := f.Close(); err != nil {
			return err
		}
		c.Operator = f.Name()
	}
	if c.Operator == "" && c.ManifestDir != "" {
		log.Errorf("Deploying CEOSLab controller using the directory 'manifests' field (%v) is deprecated, instead provide the filepath of the operator file directly using the 'operator' field going forward", c.ManifestDir)
		c.Operator = filepath.Join(c.ManifestDir, "manifest.yaml")
	}
	log.Infof("Deploying CEOSLab controller from: %s", c.Operator)
	if err := run.LogCommand("kubectl", "apply", "-f", c.Operator); err != nil {
		return fmt.Errorf("failed to deploy ceoslab operator: %w", err)
	}
	log.Infof("CEOSLab controller deployed")
	return nil
}

func (c *CEOSLabSpec) Healthy(ctx context.Context) error {
	return deploymentHealthy(ctx, c.kClient, "arista-ceoslab-operator-system")
}

func init() {
	load.Register("Lemming", &load.Spec{
		Type: LemmingSpec{},
		Tag:  "controllers",
	})
}

type LemmingSpec struct {
	ManifestDir  string `yaml:"manifests"`
	Operator     string `yaml:"operator" kne:"yaml"`
	OperatorData []byte
	kClient      kubernetes.Interface
}

func (l *LemmingSpec) SetKClient(k kubernetes.Interface) {
	l.kClient = k
}

func (l *LemmingSpec) Deploy(ctx context.Context) error {
	if l.OperatorData != nil {
		f, err := os.CreateTemp("", "lemming-operator-*.yaml")
		if err != nil {
			return err
		}
		defer os.Remove(f.Name())
		if _, err := f.Write(l.OperatorData); err != nil {
			return err
		}
		if err := f.Close(); err != nil {
			return err
		}
		l.Operator = f.Name()
	}
	if l.Operator == "" && l.ManifestDir != "" {
		log.Errorf("Deploying Lemming controller using the directory 'manifests' field (%v) is deprecated, instead provide the filepath of the operator file directly using the 'operator' field going forward", l.ManifestDir)
		l.Operator = filepath.Join(l.ManifestDir, "manifest.yaml")
	}
	log.Infof("Deploying Lemming controller from: %s", l.Operator)
	if err := run.LogCommand("kubectl", "apply", "-f", l.Operator); err != nil {
		return fmt.Errorf("failed to deploy lemming operator: %w", err)
	}
	log.Infof("Lemming controller deployed")
	return nil
}

func (l *LemmingSpec) Healthy(ctx context.Context) error {
	return deploymentHealthy(ctx, l.kClient, "lemming-operator")
}

func init() {
	load.Register("SRLinux", &load.Spec{
		Type: SRLinuxSpec{},
		Tag:  "controllers",
	})
}

type SRLinuxSpec struct {
	ManifestDir  string `yaml:"manifests"`
	Operator     string `yaml:"operator" kne:"yaml"`
	OperatorData []byte
	kClient      kubernetes.Interface
}

func (s *SRLinuxSpec) SetKClient(k kubernetes.Interface) {
	s.kClient = k
}

func (s *SRLinuxSpec) Deploy(ctx context.Context) error {
	if s.OperatorData != nil {
		f, err := os.CreateTemp("", "srlinux-operator-*.yaml")
		if err != nil {
			return err
		}
		defer os.Remove(f.Name())
		if _, err := f.Write(s.OperatorData); err != nil {
			return err
		}
		if err := f.Close(); err != nil {
			return err
		}
		s.Operator = f.Name()
	}
	if s.Operator == "" && s.ManifestDir != "" {
		log.Errorf("Deploying SRLinux controller using the directory 'manifests' field (%v) is deprecated, instead provide the filepath of the operator file directly using the 'operator' field going forward", s.ManifestDir)
		s.Operator = filepath.Join(s.ManifestDir, "manifest.yaml")
	}
	log.Infof("Deploying SRLinux controller from: %s", s.Operator)
	if err := run.LogCommand("kubectl", "apply", "-f", s.Operator); err != nil {
		return fmt.Errorf("failed to deploy srlinux operator: %w", err)
	}
	log.Infof("SRLinux controller deployed")
	return nil
}

func (s *SRLinuxSpec) Healthy(ctx context.Context) error {
	return deploymentHealthy(ctx, s.kClient, "srlinux-controller")
}

func init() {
	load.Register("IxiaTG", &load.Spec{
		Type: IxiaTGSpec{},
		Tag:  "controllers",
	})
}

type IxiaTGSpec struct {
	ManifestDir   string `yaml:"manifests"`
	Operator      string `yaml:"operator" kne:"yaml"`
	OperatorData  []byte
	ConfigMap     string `yaml:"configMap" kne:"yaml"`
	ConfigMapData []byte
	kClient       kubernetes.Interface
}

func (i *IxiaTGSpec) SetKClient(k kubernetes.Interface) {
	i.kClient = k
}

func (i *IxiaTGSpec) Deploy(ctx context.Context) error {
	if i.OperatorData != nil {
		f, err := os.CreateTemp("", "ixiatg-operator-*.yaml")
		if err != nil {
			return err
		}
		defer os.Remove(f.Name())
		if _, err := f.Write(i.OperatorData); err != nil {
			return err
		}
		if err := f.Close(); err != nil {
			return err
		}
		i.Operator = f.Name()
	}
	if i.Operator == "" && i.ManifestDir != "" {
		log.Errorf("Deploying IxiaTG controller using the directory 'manifests' field (%v) is deprecated, instead provide the filepath of the operator file directly using the 'operator' field going forward", i.ManifestDir)
		i.Operator = filepath.Join(i.ManifestDir, "ixiatg-operator.yaml")
	}
	log.Infof("Deploying IxiaTG controller from: %s", i.Operator)
	if err := run.LogCommand("kubectl", "apply", "-f", i.Operator); err != nil {
		return fmt.Errorf("failed to deploy ixiatg operator: %w", err)
	}

	if i.ConfigMap == "" && i.ManifestDir != "" {
		for _, name := range []string{"ixiatg-configmap.yaml", "ixia-configmap.yaml"} {
			path := filepath.Join(i.ManifestDir, name)
			if _, err := os.Stat(path); err == nil {
				log.Errorf("Deploying IxiaTG configmap using the directory 'manifests' field (%v) is deprecated, instead provide the filepath of the configmap file directly using the 'configMap' field going forward", i.ManifestDir)
				i.ConfigMap = path
				break
			}
		}
	}

	if i.ConfigMap == "" && i.ConfigMapData == nil {
		log.Warningf("IxiaTG controller deployed without configmap, before creating a topology with ixia-c be sure to create a configmap following https://github.com/open-traffic-generator/keng-operator#keng-operator and apply it using 'kubectl apply -f ixiatg-configmap.yaml'")
		return nil
	}
	if i.ConfigMapData != nil {
		f, err := os.CreateTemp("", "ixiatg-configmap-*.yaml")
		if err != nil {
			return err
		}
		defer os.Remove(f.Name())
		if _, err := f.Write(i.ConfigMapData); err != nil {
			return err
		}
		if err := f.Close(); err != nil {
			return err
		}
		i.ConfigMap = f.Name()
	}
	log.Infof("Deploying IxiaTG config map from: %s", i.ConfigMap)
	if err := run.LogCommand("kubectl", "apply", "-f", i.ConfigMap); err != nil {
		return fmt.Errorf("failed to deploy ixiatg config map: %w", err)
	}
	log.Infof("IxiaTG controller deployed")
	return nil
}

func (i *IxiaTGSpec) Healthy(ctx context.Context) error {
	return deploymentHealthy(ctx, i.kClient, "ixiatg-op-system")
}

func init() {
	load.Register("Cdnos", &load.Spec{
		Type: CdnosSpec{},
		Tag:  "controllers",
	})
}

type CdnosSpec struct {
	Operator     string `yaml:"operator" kne:"yaml"`
	OperatorData []byte
	kClient      kubernetes.Interface
}

func (c *CdnosSpec) SetKClient(k kubernetes.Interface) {
	c.kClient = k
}

func (c *CdnosSpec) Deploy(ctx context.Context) error {
	if c.OperatorData != nil {
		f, err := os.CreateTemp("", "cdnos-operator-*.yaml")
		if err != nil {
			return err
		}
		defer os.Remove(f.Name())
		if _, err := f.Write(c.OperatorData); err != nil {
			return err
		}
		if err := f.Close(); err != nil {
			return err
		}
		c.Operator = f.Name()
	}
	log.Infof("Deploying Cdnos controller from: %s", c.Operator)
	if err := run.LogCommand("kubectl", "apply", "-f", c.Operator); err != nil {
		return fmt.Errorf("failed to deploy cdnos operator: %w", err)
	}
	log.Infof("Cdnos controller deployed")
	return nil
}

func (c *CdnosSpec) Healthy(ctx context.Context) error {
	return deploymentHealthy(ctx, c.kClient, "cdnos-controller-system")
}

func deploymentHealthy(ctx context.Context, c kubernetes.Interface, name string) error {
	log.Infof("Waiting on deployment %q to be healthy", name)
	w, err := c.AppsV1().Deployments(name).Watch(ctx, metav1.ListOptions{})
	if err != nil {
		return fmt.Errorf("failed to create watcher for deployment %q", name)
	}
	ch := w.ResultChan()
	for {
		select {
		case <-ctx.Done():
			return fmt.Errorf("context canceled before %q healthy", name)
		case e, ok := <-ch:
			if !ok {
				return fmt.Errorf("watch channel closed before %q healthy", name)
			}
			d, ok := e.Object.(*appsv1.Deployment)
			if !ok {
				return fmt.Errorf("invalid object type: %T", d)
			}
			var r int32 = 1
			if d.Spec.Replicas != nil {
				r = *d.Spec.Replicas
			}
			if d.Status.AvailableReplicas == r &&
				d.Status.ReadyReplicas == r &&
				d.Status.UnavailableReplicas == 0 &&
				d.Status.Replicas == r &&
				d.Status.UpdatedReplicas == r {
				log.Infof("Deployment %q healthy", name)
				return nil
			}
		}
	}
}
