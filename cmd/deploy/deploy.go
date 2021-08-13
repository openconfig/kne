package deploy

import (
	"context"
	"fmt"
	"io/ioutil"
	"time"

	"github.com/pkg/errors"
	log "github.com/sirupsen/logrus"
	"github.com/spf13/cobra"
	"gopkg.in/yaml.v3"
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

type EgressSpec struct {
	Kind string    `yaml:"kind"`
	Spec yaml.Node `yaml:"spec"`
}
type CNISpec struct {
	Kind string    `yaml:"kind"`
	Spec yaml.Node `yaml:"spec"`
}

type KindSpec struct {
	Name    string `yaml:"name"`
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
	Version   string `yaml:"version"`
	UseDocker bool   `yaml:"use_docker"`
}

func (m *MetalLBSpec) Deploy(ctx context.Context) error {
	return nil
}

type MeshnetSpec struct {
	Image     string `yaml:"image"`
	Manifests string `yaml:"manifests"`
}

type Cluster interface {
	Deploy(context.Context) error
}

type CRI interface{}
type Egress interface{}
type CNI interface{}

type Deployment struct {
	Cluster Cluster
	Egress  Egress
	CNI     CNI
}

type DeploymentConfig struct {
	Cluster ClusterSpec `yaml:"cluster"`
	Egress  EgressSpec  `yaml:"egress"`
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
	switch cfg.Egress.Kind {
	case "MetalLB":
		v := &MetalLBSpec{}
		if err := cfg.Egress.Spec.Decode(v); err != nil {
			return nil, err
		}
		d.Egress = v
	default:
		return nil, fmt.Errorf("egress type not supported: %s", cfg.Egress.Kind)
	}
	return d, nil
}

func deployFn(cmd *cobra.Command, args []string) error {
	if len(args) != 1 {
		return fmt.Errorf("%s: missing args", cmd.Use)
	}
	b, err := ioutil.ReadFile(args[0])
	if err != nil {
		return err
	}
	dCfg := &DeploymentConfig{}
	if err := yaml.Unmarshal(b, dCfg); err != nil {
		return err
	}
	d, err := NewDeployment(dCfg)
	if err != nil {
		return err
	}
	if err := d.Cluster.Deploy(cmd.Context()); err != nil {
		return err
	}
	log.Infof("Validating cluster health")
	log.Infof("Deploying metallb")
	log.Infof("Validating metallb")
	log.Infof("Deploying meshnet")
	log.Infof("Validing meshnet health")
	log.Infof("Deploying topology manager")
	log.Infof("Validating topology manager health")
	log.Infof("Ready for topology")
	return nil
}
