package deploy

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"text/template"
	"time"

	dtypes "github.com/docker/docker/api/types"
	dclient "github.com/docker/docker/client"
	kexec "github.com/google/kne/os/exec"
	"github.com/pkg/errors"
	log "github.com/sirupsen/logrus"
	"gopkg.in/yaml.v3"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/fields"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/tools/clientcmd"
	"sigs.k8s.io/kind/pkg/cluster"
)

const (
	dockerConfigEnvVar           = "DOCKER_CONFIG"
	kubeletConfigPathTemplate    = "%s:/var/lib/kubelet/config.json"
	dockerConfigTemplateContents = `{
  "auths": {
{{range $val := .}}    "{{$val}}": {}
{{end}}  }
}
`
)

var (
	dockerConfigTemplate = template.Must(template.New("dockerConfig").Parse(dockerConfigTemplateContents))
	logOut               = log.StandardLogger().Out

	// execer handles all execs on host.
	execer execerInterface = kexec.NewExecer(logOut, logOut)

	// Stubs for testing.
	newProvider  = defaultProvider
	execLookPath = exec.LookPath
)

//go:generate mockgen -source=specs.go -destination=mocks/mock_provider.go -package=mocks provider
type provider interface {
	List() ([]string, error)
	Create(name string, options ...cluster.CreateOption) error
}

func defaultProvider() provider {
	return cluster.NewProvider(cluster.ProviderWithLogger(&logAdapter{log.StandardLogger()}))
}

type execerInterface interface {
	Exec(string, ...string) error
	SetStdout(io.Writer)
	SetStderr(io.Writer)
}

type Cluster interface {
	Deploy(context.Context) error
	Delete() error
	Healthy() error
	GetName() string
}

type Ingress interface {
	Deploy(context.Context) error
	SetKClient(kubernetes.Interface)
	Healthy(context.Context) error
}

type CNI interface {
	Deploy(context.Context) error
	SetKClient(kubernetes.Interface)
	Healthy(context.Context) error
}

type Deployment struct {
	Cluster Cluster
	Ingress Ingress
	CNI     CNI
}

func (d *Deployment) String() string {
	b, _ := json.MarshalIndent(d, "", "\t")
	return string(b)
}

func (d *Deployment) Deploy(ctx context.Context, kubecfg string) error {
	log.Infof("Deploying cluster...")
	if err := d.Cluster.Deploy(ctx); err != nil {
		return err
	}
	log.Infof("Cluster deployed")
	// Once cluster is up set kClient
	rCfg, err := clientcmd.BuildConfigFromFlags("", kubecfg)
	if err != nil {
		return err
	}
	kClient, err := kubernetes.NewForConfig(rCfg)
	if err != nil {
		return err
	}
	d.Ingress.SetKClient(kClient)
	log.Infof("Deploying ingress...")
	if err := d.Ingress.Deploy(ctx); err != nil {
		return err
	}
	tCtx, cancel := context.WithTimeout(ctx, 1*time.Minute)
	defer cancel()
	if err := d.Ingress.Healthy(tCtx); err != nil {
		return err
	}
	log.Infof("Ingress healthy")
	log.Infof("Deploying CNI...")
	if err := d.CNI.Deploy(ctx); err != nil {
		return err
	}
	d.CNI.SetKClient(kClient)
	tCtx, cancel = context.WithTimeout(ctx, 1*time.Minute)
	defer cancel()
	if err := d.CNI.Healthy(tCtx); err != nil {
		return err
	}
	log.Infof("CNI healthy")
	return nil
}

func (d *Deployment) Delete() error {
	log.Infof("Deleting cluster...")
	if err := d.Cluster.Delete(); err != nil {
		return err
	}
	log.Infof("Cluster deleted")
	return nil
}

func (d *Deployment) Healthy(ctx context.Context) error {
	if err := d.Cluster.Healthy(); err != nil {
		return err
	}
	log.Infof("Cluster healthy")
	tCtx, cancel := context.WithTimeout(ctx, 1*time.Minute)
	defer cancel()
	if err := d.Ingress.Healthy(tCtx); err != nil {
		return err
	}
	log.Infof("Ingress healthy")
	tCtx, cancel = context.WithTimeout(ctx, 1*time.Minute)
	defer cancel()
	if err := d.CNI.Healthy(tCtx); err != nil {
		return err
	}
	log.Infof("CNI healthy")
	return nil
}

type KindSpec struct {
	Name                     string            `yaml:"name"`
	Recycle                  bool              `yaml:"recycle"`
	Version                  string            `yaml:"version"`
	Image                    string            `yaml:"image"`
	Retain                   bool              `yaml:"retain"`
	Wait                     time.Duration     `yaml:"wait"`
	Kubecfg                  string            `yaml:"kubecfg"`
	DeployWithClient         bool              `yaml:"deployWithClient"`
	GoogleArtifactRegistries []string          `yaml:"googleArtifactRegistries"`
	ContainerImages          map[string]string `yaml:"containerImages"`
}

func (k *KindSpec) Deploy(ctx context.Context) error {
	provider := newProvider()
	if k.Recycle {
		clusters, err := provider.List()
		if err != nil {
			return err
		}
		for _, v := range clusters {
			if k.Name == v {
				log.Infof("Recycling existing cluster: %s", v)
				return nil
			}
		}
	}
	if k.DeployWithClient {
		if len(k.GoogleArtifactRegistries) != 0 {
			return fmt.Errorf("setting up access to artifact registries %v requires unsetting the deployWithClient field", k.GoogleArtifactRegistries)
		}
		if len(k.ContainerImages) != 0 {
			return fmt.Errorf("loading container images requires unsetting the deployWithClient field")
		}
		if err := provider.Create(
			k.Name,
			cluster.CreateWithNodeImage(k.Image),
			cluster.CreateWithRetain(k.Retain),
			cluster.CreateWithWaitForReady(k.Wait),
			cluster.CreateWithKubeconfigPath(k.Kubecfg),
			cluster.CreateWithDisplayUsage(true),
			cluster.CreateWithDisplaySalutation(true),
		); err != nil {
			return errors.Wrap(err, "failed to create cluster using kind client")
		}
		log.Infof("Deployed kind cluster using kind client: %s", k.Name)
		return nil
	}
	if _, err := execLookPath("kind"); err != nil {
		return errors.Wrap(err, "install kind cli to deploy, or set the deployWithClient field")
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
	if err := execer.Exec("kind", args...); err != nil {
		return errors.Wrap(err, "failed to create cluster using cli")
	}
	log.Infof("Deployed kind cluster: %s", k.Name)
	if len(k.GoogleArtifactRegistries) != 0 {
		log.Infof("Setting up Google Artifact Registry access for %v", k.GoogleArtifactRegistries)
		if err := k.setupGoogleArtifactRegistryAccess(); err != nil {
			return errors.Wrap(err, "setting up google artifact registry access")
		}
	}
	if len(k.ContainerImages) != 0 {
		log.Infof("Loading container images")
		if err := k.loadContainerImages(); err != nil {
			return errors.Wrap(err, "loading container images")
		}
	}
	return nil
}

func (k *KindSpec) Delete() error {
	if _, err := execLookPath("kind"); err != nil {
		return errors.Wrap(err, "install kind cli to delete")
	}
	args := []string{"delete", "cluster"}
	if k.Name != "" {
		args = append(args, "--name", k.Name)
	}
	if err := execer.Exec("kind", args...); err != nil {
		return errors.Wrap(err, "failed to delete cluster using cli")
	}
	return nil
}

func (k *KindSpec) Healthy() error {
	if _, err := exec.LookPath("kubectl"); err != nil {
		return errors.Wrap(err, "install kubectl to check health")
	}
	if err := execer.Exec("kubectl", "cluster-info", "--context", fmt.Sprintf("kind-%s", k.GetName())); err != nil {
		return errors.Wrap(err, "cluster not healthy")
	}
	return nil
}

func (k *KindSpec) GetName() string {
	if k.Name != "" {
		return k.Name
	}
	return "kind"
}

func (k *KindSpec) setupGoogleArtifactRegistryAccess() error {
	if _, err := execLookPath("gcloud"); err != nil {
		return errors.Wrap(err, "install gcloud cli to setup Google Artifact Registry access")
	}
	if _, err := execLookPath("docker"); err != nil {
		return errors.Wrap(err, "install docker cli to setup Google Artifact Registry access")
	}
	// Create a temporary dir to hold a new docker config that lacks credsStore.
	// Then use `docker login` to store the generated credentials directly in
	// the temporary docker config.
	// See https://kind.sigs.k8s.io/docs/user/private-registries/#use-an-access-token
	// for more information.
	tempDockerDir, err := os.MkdirTemp("", "kne_kind_docker")
	if err != nil {
		return err
	}
	defer os.RemoveAll(tempDockerDir)
	originalConfig := os.Getenv(dockerConfigEnvVar)
	defer os.Setenv(dockerConfigEnvVar, originalConfig)
	if err := os.Setenv(dockerConfigEnvVar, tempDockerDir); err != nil {
		return err
	}
	configPath := filepath.Join(tempDockerDir, "config.json")
	if err := writeDockerConfig(configPath, k.GoogleArtifactRegistries); err != nil {
		return err
	}
	var token bytes.Buffer
	execer.SetStdout(&token)
	if err := execer.Exec("gcloud", "auth", "print-access-token"); err != nil {
		return err
	}
	execer.SetStdout(log.StandardLogger().Out)
	for _, r := range k.GoogleArtifactRegistries {
		s := fmt.Sprintf("https://%s", r)
		if err := execer.Exec("docker", "login", "-u", "oauth2accesstoken", "-p", token.String(), s); err != nil {
			return err
		}
	}
	args := []string{"get", "nodes"}
	if k.Name != "" {
		args = append(args, "--name", k.Name)
	}
	var nodes bytes.Buffer
	execer.SetStdout(&nodes)
	if err := execer.Exec("kind", args...); err != nil {
		return err
	}
	execer.SetStdout(log.StandardLogger().Out)
	// Copy the new docker config to each node and restart kubelet so it
	// picks up the new config that contains the embedded credentials.
	for _, node := range strings.Split(nodes.String(), " ") {
		node = strings.TrimSuffix(node, "\n")
		if err := execer.Exec("docker", "cp", configPath, fmt.Sprintf(kubeletConfigPathTemplate, node)); err != nil {
			return err
		}
		if err := execer.Exec("docker", "exec", node, "systemctl", "restart", "kubelet.service"); err != nil {
			return err
		}
	}
	log.Infof("Setup credentials for accessing GAR locations %v in kind cluster", k.GoogleArtifactRegistries)
	return nil
}

func (k *KindSpec) loadContainerImages() error {
	if _, err := execLookPath("docker"); err != nil {
		return errors.Wrap(err, "install docker cli to load container images")
	}
	for s, d := range k.ContainerImages {
		log.Infof("Loading %q as %q", s, d)
		if err := execer.Exec("docker", "pull", s); err != nil {
			return errors.Wrapf(err, "pulling %q", s)
		}
		if err := execer.Exec("docker", "tag", s, d); err != nil {
			return errors.Wrapf(err, "tagging %q with %q", s, d)
		}
		args := []string{"load", "docker-image", d}
		if k.Name != "" {
			args = append(args, "--name", k.Name)
		}
		if err := execer.Exec("kind", args...); err != nil {
			return errors.Wrapf(err, "loading %q", d)
		}
	}
	log.Infof("Loaded all container images")
	return nil
}

func writeDockerConfig(path string, registries []string) error {
	f, err := os.Create(path)
	if err != nil {
		return err
	}
	defer f.Close()
	return dockerConfigTemplate.Execute(f, registries)
}

type MetalLBSpec struct {
	Version     string `yaml:"version"`
	IPCount     int    `yaml:"ip_count"`
	ManifestDir string `yaml:"manifests"`
	kClient     kubernetes.Interface
	dClient     dclient.NetworkAPIClient
}

func (m *MetalLBSpec) SetKClient(c kubernetes.Interface) {
	m.kClient = c
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

type pool struct {
	Name      string   `yaml:"name"`
	Protocol  string   `yaml:"protocol"`
	Addresses []string `yaml:"addresses"`
}

type metalLBConfig struct {
	AddressPools []pool `yaml:"address-pools"`
}

func makeConfig(n *net.IPNet, count int) metalLBConfig {
	start := make(net.IP, len(n.IP))
	copy(start, n.IP)
	inc(start, 50)
	end := make(net.IP, len(start))
	copy(end, start)
	inc(end, count)
	return metalLBConfig{
		AddressPools: []pool{{
			Name:      "default",
			Protocol:  "layer2",
			Addresses: []string{fmt.Sprintf("%s - %s", start, end)},
		}},
	}
}

func (m *MetalLBSpec) Deploy(ctx context.Context) error {
	if m.dClient == nil {
		var err error
		m.dClient, err = dclient.NewClientWithOpts(dclient.FromEnv)
		if err != nil {
			return err
		}
	}
	log.Infof("Creating metallb namespace")
	if err := execer.Exec("kubectl", "apply", "-f", filepath.Join(m.ManifestDir, "namespace.yaml")); err != nil {
		return err
	}
	_, err := m.kClient.CoreV1().Secrets("metallb-system").Get(ctx, "memberlist", metav1.GetOptions{})
	if err != nil {
		log.Infof("Creating metallb secret")
		d := make([]byte, 16)
		rand.Read(d)
		s := &corev1.Secret{
			ObjectMeta: metav1.ObjectMeta{
				Name: "memberlist",
			},
			StringData: map[string]string{
				"secretkey": base64.StdEncoding.EncodeToString(d),
			},
		}
		_, err := m.kClient.CoreV1().Secrets("metallb-system").Create(ctx, s, metav1.CreateOptions{})
		if err != nil {
			return err
		}
	}
	log.Infof("Applying metallb pods")
	if err := execer.Exec("kubectl", "apply", "-f", filepath.Join(m.ManifestDir, "metallb.yaml")); err != nil {
		return err
	}
	_, err = m.kClient.CoreV1().ConfigMaps("metallb-system").Get(ctx, "config", metav1.GetOptions{})
	if err != nil {
		log.Infof("Applying metallb ingress config")
		// Get Network information from docker.
		nr, err := m.dClient.NetworkList(ctx, dtypes.NetworkListOptions{})
		if err != nil {
			return err
		}
		var network dtypes.NetworkResource
		for _, v := range nr {
			if v.Name == "kind" {
				network = v
				break
			}
		}
		var n *net.IPNet
		for _, ipRange := range network.IPAM.Config {
			_, ipNet, err := net.ParseCIDR(ipRange.Subnet)
			if err != nil {
				return err
			}
			if ipNet.IP.To4() != nil {
				n = ipNet
				break
			}
		}
		if n == nil {
			return fmt.Errorf("failed to find kind ipv4 docker net")
		}
		config := makeConfig(n, m.IPCount)
		b, err := yaml.Marshal(config)
		if err != nil {
			return err
		}
		cm := &corev1.ConfigMap{
			ObjectMeta: metav1.ObjectMeta{
				Name: "config",
			},
			Data: map[string]string{
				"config": string(b),
			},
		}
		_, err = m.kClient.CoreV1().ConfigMaps("metallb-system").Create(ctx, cm, metav1.CreateOptions{})
		if err != nil {
			return err
		}
	}
	return nil
}

func (m *MetalLBSpec) Healthy(ctx context.Context) error {
	log.Infof("Waiting on Metallb to be Healthy")
	w, err := m.kClient.AppsV1().Deployments("metallb-system").Watch(ctx, metav1.ListOptions{})
	if err != nil {
		return err
	}
	ch := w.ResultChan()
	for {
		select {
		case <-ctx.Done():
			return fmt.Errorf("context canceled before healthy")
		case e, ok := <-ch:
			if !ok {
				return fmt.Errorf("watch channel closed before healthy")
			}
			d, ok := e.Object.(*appsv1.Deployment)
			if !ok {
				return fmt.Errorf("invalid object type: %T", d)
			}
			if d.Status.AvailableReplicas == 1 &&
				d.Status.ReadyReplicas == 1 &&
				d.Status.UnavailableReplicas == 0 &&
				d.Status.Replicas == 1 &&
				d.Status.UpdatedReplicas == 1 {
				log.Infof("Metallb Healthy")
				return nil
			}
		}
	}
}

type MeshnetSpec struct {
	Image       string `yaml:"image"`
	ManifestDir string `yaml:"manifests"`
	kClient     kubernetes.Interface
}

func (m *MeshnetSpec) SetKClient(c kubernetes.Interface) {
	m.kClient = c
}

func (m *MeshnetSpec) Deploy(ctx context.Context) error {
	log.Infof("Deploying Meshnet from: %s", m.ManifestDir)
	if err := execer.Exec("kubectl", "apply", "-k", m.ManifestDir); err != nil {
		return err
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
			return fmt.Errorf("context canceled before healthy")
		case e, ok := <-w.ResultChan():
			if !ok {
				return fmt.Errorf("watch channel closed before healthy")
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
