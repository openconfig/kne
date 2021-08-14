package deploy

import (
	"context"
	"fmt"
	"io/ioutil"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"time"

	dtypes "github.com/docker/docker/api/types"
	dclient "github.com/docker/docker/client"
	"github.com/pkg/errors"
	log "github.com/sirupsen/logrus"
	"github.com/spf13/cobra"
	"gopkg.in/yaml.v3"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/fields"

	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/tools/clientcmd"
	"sigs.k8s.io/kind/pkg/cluster"
	klog "sigs.k8s.io/kind/pkg/log"
)

func New() *cobra.Command {
	deployCmd := &cobra.Command{
		Use:   "deploy <deployment yaml>",
		Short: "Deploy cluster.",
		RunE:  deployFn,
	}
	return deployCmd
}

type ClusterSpec struct {
	Kind string    `yaml:"kind"`
	Spec yaml.Node `yaml:"spec"`
}

type IngressSpec struct {
	Kind string    `yaml:"kind"`
	Spec yaml.Node `yaml:"spec"`
}
type CNISpec struct {
	Kind string    `yaml:"kind"`
	Spec yaml.Node `yaml:"spec"`
}

type KindSpec struct {
	Name    string `yaml:"name"`
	Recycle bool   `yaml:"recycle"`
	Version string `yaml:"version"`
	Image   string `yaml:"image"`
	Retain  bool   `yaml:"retain"`
	Wait    time.Duration
	Kubecfg string `yaml:"kubecfg"`
}

type logAdapter struct {
	*log.Logger
}

func (l *logAdapter) Error(msg string) {
	l.Logger.Error(msg)
}

func (l *logAdapter) Info(msg string) {
	l.Logger.Info(msg)
}

func (l *logAdapter) Warn(msg string) {
	l.Logger.Warn(msg)
}

func (l *logAdapter) Enabled() bool {
	return true
}

type debugLogger struct {
	*log.Logger
}

func (l *debugLogger) Info(msg string) {
	l.Logger.Debug(msg)
}

func (l *debugLogger) Infof(fmt string, args ...interface{}) {
	l.Logger.Debugf(fmt, args...)
}

func (l *debugLogger) Enabled() bool {
	return true
}

func (l *logAdapter) V(level klog.Level) klog.InfoLogger {
	if level == 0 {
		return l
	}
	return &debugLogger{l.Logger}
}

func (k *KindSpec) Deploy(ctx context.Context) error {
	provider := cluster.NewProvider(cluster.ProviderWithLogger(&logAdapter{log.StandardLogger()}))
	if k.Recycle {
		clusters, err := provider.List()
		if err != nil {
			return err
		}
		for _, v := range clusters {
			if k.Name == v {
				log.Infof("recycling existing cluster: %s", v)
				return nil
			}
		}
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
		return errors.Wrap(err, "failed to create cluster")
	}
	log.Infof("Deployed Kind cluster: %s", k.Name)
	return nil
}

type MetalLBSpec struct {
	Version     string `yaml:"version"`
	IPCount     int    `yaml:"ip_count"`
	ManifestDir string `yaml:"manifests"`
	kClient     kubernetes.Interface
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

func execCmd(cmd string, args ...string) error {
	c := exec.Command(cmd, args...)
	c.Stderr = os.Stderr
	c.Stdout = os.Stdout
	log.Info(c.String())
	if err := c.Run(); err != nil {
		return fmt.Errorf("%q failed: %v", c.String(), err)
	}
	return nil
}

func (m *MetalLBSpec) Deploy(ctx context.Context) error {
	mPath := filepath.Join(deploymentBasePath, m.ManifestDir)
	log.Infof("Deploying metallb from: %s", mPath)
	if err := execCmd("kubectl", "apply", "-f", filepath.Join(mPath, "namespace.yaml")); err != nil {
		return err
	}
	if err := execCmd("kubectl", "create", "secret", "generic", "-n", "metallb-system", "memberlist", "--from-literal=secretkey=\"$(openssl rand -base64 128)\""); err != nil {
		return err
	}
	if err := execCmd("kubectl", "apply", "-f", filepath.Join(mPath, "metallb.yaml")); err != nil {
		return err
	}
	c, err := dclient.NewClientWithOpts(dclient.FromEnv)
	if err != nil {
		return err
	}
	nr, err := c.NetworkList(ctx, dtypes.NetworkListOptions{})
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
	start := make(net.IP, len(n.IP))
	copy(start, n.IP)
	inc(start, 50)
	end := make(net.IP, len(start))
	copy(end, start)
	inc(end, m.IPCount)
	config := metalLBConfig{
		AddressPools: []pool{{
			Name:      "default",
			Protocol:  "layer2",
			Addresses: []string{fmt.Sprintf("%s - %s", start, end)},
		}},
	}
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
	return nil
}

func (m *MetalLBSpec) Healthy(ctx context.Context) error {
	log.Infof("Waiting on Metallb to be Healthy")
	w, err := m.kClient.AppsV1().Deployments("metallb-system").Watch(ctx, metav1.ListOptions{})
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
	mPath := filepath.Join(deploymentBasePath, m.ManifestDir)
	log.Infof("Deploying Meshnet from: %s", mPath)
	if err := execCmd("kubectl", "apply", "-k", mPath); err != nil {
		return err
	}
	log.Infof("Meshnet Deployed")
	return nil
}

func (m *MeshnetSpec) Healthy(ctx context.Context) error {
	log.Infof("Waiting on Metallb to be Healthy")
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

type Cluster interface {
	Deploy(context.Context) error
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

type DeploymentConfig struct {
	Cluster ClusterSpec `yaml:"cluster"`
	Ingress IngressSpec `yaml:"ingress"`
	CNI     CNISpec     `yaml:"cni"`
}

func NewDeployment(cfg *DeploymentConfig) (*Deployment, error) {
	d := &Deployment{}
	switch cfg.Cluster.Kind {
	case "Kind":
		log.Infof("Using kind scenario")
		v := &KindSpec{}
		if err := cfg.Cluster.Spec.Decode(v); err != nil {
			return nil, err
		}
		d.Cluster = v
	default:
		return nil, fmt.Errorf("cluster type not supported: %s", cfg.Cluster.Kind)
	}
	switch cfg.CNI.Kind {
	case "Meshnet":
		v := &MeshnetSpec{}
		if err := cfg.CNI.Spec.Decode(v); err != nil {
			return nil, err
		}
		d.CNI = v
	default:
		return nil, fmt.Errorf("CNI type not supported: %s", cfg.CNI.Kind)
	}
	switch cfg.Ingress.Kind {
	case "MetalLB":
		v := &MetalLBSpec{}
		if err := cfg.Ingress.Spec.Decode(v); err != nil {
			return nil, err
		}
		d.Ingress = v
	default:
		return nil, fmt.Errorf("ingress type not supported: %s", cfg.Ingress.Kind)
	}
	return d, nil
}

var (
	deploymentBasePath string
)

func deploymentFromArg(p string) (*DeploymentConfig, string, error) {
	deploymentBasePath, err := filepath.Abs(p)
	if err != nil {
		return nil, "", err
	}
	b, err := ioutil.ReadFile(deploymentBasePath)
	if err != nil {
		return nil, "", err
	}
	deploymentBasePath = filepath.Dir(deploymentBasePath)
	dCfg := &DeploymentConfig{}
	if err := yaml.Unmarshal(b, dCfg); err != nil {
		return nil, "", err
	}
	return dCfg, deploymentBasePath, nil
}

func deployFn(cmd *cobra.Command, args []string) error {
	if len(args) != 1 {
		return fmt.Errorf("%s: missing args", cmd.Use)
	}
	dCfg, bp, err := deploymentFromArg(args[0])
	if err != nil {
		return err
	}
	deploymentBasePath = bp
	d, err := NewDeployment(dCfg)
	if err != nil {
		return err
	}
	if err := d.Cluster.Deploy(cmd.Context()); err != nil {
		return err
	}
	// Once cluster is up set kClient
	kubecfg, err := cmd.Flags().GetString("kubecfg")
	if err != nil {
		return err
	}
	rCfg, err := clientcmd.BuildConfigFromFlags("", kubecfg)
	if err != nil {
		return err
	}
	kClient, err := kubernetes.NewForConfig(rCfg)
	if err != nil {
		return err
	}
	d.Ingress.SetKClient(kClient)
	log.Infof("Validating cluster health")
	if err := d.Ingress.Deploy(cmd.Context()); err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(cmd.Context(), 1*time.Minute)
	defer cancel()
	if err := d.Ingress.Healthy(ctx); err != nil {
		return err
	}
	if err := d.CNI.Deploy(cmd.Context()); err != nil {
		return err
	}
	d.CNI.SetKClient(kClient)
	ctx, cancel = context.WithTimeout(cmd.Context(), 1*time.Minute)
	defer cancel()
	if err := d.CNI.Healthy(ctx); err != nil {
		return err
	}
	log.Infof("Ready for topology")
	return nil
}
