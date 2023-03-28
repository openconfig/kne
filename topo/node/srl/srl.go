package srl

import (
	"context"
	"errors"
	"fmt"
	"io"
	"path/filepath"

	topopb "github.com/openconfig/kne/proto/topo"
	"github.com/openconfig/kne/topo/node"
	scraplinetwork "github.com/scrapli/scrapligo/driver/network"
	scrapliopopts "github.com/scrapli/scrapligo/driver/opoptions"
	scrapliopts "github.com/scrapli/scrapligo/driver/options"
	scrapliutil "github.com/scrapli/scrapligo/util"
	srlinuxv1 "github.com/srl-labs/srl-controller/api/v1"
	"github.com/srl-labs/srlinux-scrapli"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/fields"
	runtime "k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/rest"
	log "k8s.io/klog/v2"
	ctrlclient "sigs.k8s.io/controller-runtime/pkg/client"
)

const (
	scrapliPlatformName = "nokia_srl"
	configResetCmd      = "load factory auto-commit"
)

var (
	// ErrIncompatibleCliConn raised when an invalid scrapligo cli transport type is found.
	ErrIncompatibleCliConn = errors.New("incompatible cli connection in use")

	// newSrlinuxClient returns a controller-runtime client for srlinux
	// resources. This can be set to a fake for unit testing.
	newSrlinuxClient = newSrlinuxClientWithSchema
)

func New(nodeImpl *node.Impl) (node.Node, error) {
	if nodeImpl == nil {
		return nil, fmt.Errorf("nodeImpl cannot be nil")
	}
	if nodeImpl.Proto == nil {
		return nil, fmt.Errorf("nodeImpl.Proto cannot be nil")
	}
	cfg := defaults(nodeImpl.Proto)
	nodeImpl.Proto = cfg
	n := &Node{
		Impl: nodeImpl,
	}

	c, err := newSrlinuxClient(n.RestConfig)
	if err != nil {
		return nil, err
	}

	n.ControllerClient = c

	return n, nil
}

// newSrlinuxClientWithSchema returns a controller-runtime client for srlinux and loads its schema.
func newSrlinuxClientWithSchema(c *rest.Config) (ctrlclient.Client, error) {
	// initialize the controller-runtime client with srlinux scheme
	scheme := runtime.NewScheme()

	err := srlinuxv1.AddToScheme(scheme)
	if err != nil {
		return nil, err
	}

	return ctrlclient.New(c, ctrlclient.Options{Scheme: scheme})
}

type Node struct {
	*node.Impl
	cliConn *scraplinetwork.Driver

	// scrapli options used in testing
	testOpts []scrapliutil.Option

	ControllerClient ctrlclient.Client
}

// Add validations for interfaces the node provides
var (
	_ node.Certer       = (*Node)(nil)
	_ node.Resetter     = (*Node)(nil)
	_ node.ConfigPusher = (*Node)(nil)
)

// GenerateSelfSigned generates a self-signed TLS certificate using SR Linux tools command
// and creates an enclosing server profile.
func (n *Node) GenerateSelfSigned(ctx context.Context) error {
	selfSigned := n.Proto.GetConfig().GetCert().GetSelfSigned()
	if selfSigned == nil {
		log.Infof("%s - no cert config", n.Name())
		return nil
	}
	log.Infof("%s - generating self signed certs", n.Name())
	log.Infof("%s - waiting for pod to be running", n.Name())
	w, err := n.KubeClient.CoreV1().Pods(n.Namespace).Watch(ctx, metav1.ListOptions{
		FieldSelector: fields.SelectorFromSet(
			fields.Set{metav1.ObjectNameField: n.Name()},
		).String(),
	})
	if err != nil {
		return err
	}
	for e := range w.ResultChan() {
		p := e.Object.(*corev1.Pod)
		if p.Status.Phase == corev1.PodRunning {
			break
		}
	}
	log.Infof("%s - pod running.", n.Name())

	if err := n.SpawnCLIConn(); err != nil {
		return err
	}

	err = srlinux.AddSelfSignedServerTLSProfile(n.cliConn, selfSigned.CertName, false)
	if err != nil {
		return err
	}

	log.Infof("%s - finished cert generation", n.Name())

	return n.cliConn.Close()
}

// ConfigPush pushes config lines provided in r using scrapligo SendConfig
func (n *Node) ConfigPush(ctx context.Context, r io.Reader) error {
	log.Infof("%s - pushing config", n.Name())

	cfg, err := io.ReadAll(r)
	cfgs := string(cfg)

	log.V(1).Infof("config to push:\n%s", cfgs)

	if err != nil {
		return err
	}

	err = n.SpawnCLIConn()
	if err != nil {
		return err
	}

	defer n.cliConn.Close()

	resp, err := n.cliConn.SendConfig(cfgs, scrapliopopts.WithStopOnFailed())
	if err != nil {
		return err
	}

	if resp.Failed == nil {
		log.Infof("%s - finshed config push", n.Impl.Proto.Name)
	}

	return resp.Failed
}

// Create creates a Nokia SR Linux node by interfacing with srl-labs/srl-controller
func (n *Node) Create(ctx context.Context) error {
	log.Infof("Creating Srlinux node resource %s", n.Name())

	if err := n.CreateConfig(ctx); err != nil {
		return fmt.Errorf("node %s failed to create config-map %w", n.Name(), err)
	}
	log.Infof("Created SR Linux node %s configmap", n.Name())

	srl := &srlinuxv1.Srlinux{
		TypeMeta: metav1.TypeMeta{
			Kind:       "Srlinux",
			APIVersion: "kne.srlinux.dev/v1",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      n.Name(),
			Namespace: n.Namespace,
			Labels: map[string]string{
				"app":  n.Name(),
				"topo": n.Namespace,
			},
		},
		Spec: srlinuxv1.SrlinuxSpec{
			NumInterfaces: len(n.GetProto().GetInterfaces()),
			Config: &srlinuxv1.NodeConfig{
				Command:           n.GetProto().GetConfig().GetCommand(),
				Args:              n.GetProto().GetConfig().GetArgs(),
				Image:             n.GetProto().GetConfig().GetImage(),
				Env:               n.GetProto().GetConfig().GetEnv(),
				EntryCommand:      n.GetProto().GetConfig().GetEntryCommand(),
				ConfigPath:        n.GetProto().GetConfig().GetConfigPath(),
				ConfigFile:        n.GetProto().GetConfig().GetConfigFile(),
				ConfigDataPresent: n.isConfigDataPresent(),
				Cert: &srlinuxv1.CertificateCfg{
					CertName:   n.GetProto().GetConfig().GetCert().GetSelfSigned().GetCertName(),
					KeyName:    n.GetProto().GetConfig().GetCert().GetSelfSigned().GetKeyName(),
					CommonName: n.GetProto().GetConfig().GetCert().GetSelfSigned().GetCommonName(),
					KeySize:    n.GetProto().GetConfig().GetCert().GetSelfSigned().GetKeySize(),
				},
				Sleep: n.GetProto().GetConfig().GetSleep(),
			},
			Constraints: n.GetProto().GetConstraints(),
			Model:       n.GetProto().GetModel(),
			Version:     n.GetProto().GetVersion(),
		},
	}

	err := n.ControllerClient.Create(ctx, srl)
	if err != nil {
		return err
	}

	// wait till srlinux pods are created in the cluster
	w, err := n.KubeClient.CoreV1().Pods(n.Namespace).Watch(ctx, metav1.ListOptions{
		FieldSelector: fields.SelectorFromSet(fields.Set{metav1.ObjectNameField: n.Name()}).String(),
	})
	if err != nil {
		return err
	}
	for e := range w.ResultChan() {
		p := e.Object.(*corev1.Pod)
		if p.Status.Phase == corev1.PodPending {
			break
		}
	}

	log.Infof("Created Srlinux resource: %s", n.Name())

	if err := n.CreateService(ctx); err != nil {
		return err
	}

	return err
}

func (n *Node) Delete(ctx context.Context) error {
	err := n.ControllerClient.Delete(ctx, &srlinuxv1.Srlinux{ObjectMeta: metav1.ObjectMeta{Name: n.Name()}})
	if err != nil {
		return err
	}

	log.Infof("Deleted custom resource: %s", n.Name())
	if err := n.DeleteService(ctx); err != nil {
		return err
	}
	if err := n.DeleteConfig(ctx); err != nil {
		return err
	}
	log.Infof("Deleted Srlinux node resource %s", n.Name())
	return nil
}

func defaults(pb *topopb.Node) *topopb.Node {
	if pb.Config == nil {
		pb.Config = &topopb.Config{}
	}
	if pb.Services == nil {
		pb.Services = map[uint32]*topopb.Service{
			443: {
				Name:   "ssl",
				Inside: 443,
			},
			22: {
				Name:   "ssh",
				Inside: 22,
			},
			57400: {
				Name:   "gnmi",
				Inside: 57400,
			},
		}
	}
	if pb.Labels == nil {
		pb.Labels = map[string]string{}
	}
	if pb.Labels["vendor"] == "" {
		pb.Labels["vendor"] = topopb.Vendor_NOKIA.String()
	}
	if pb.Config == nil {
		pb.Config = &topopb.Config{}
	}
	if pb.Config.Image == "" {
		pb.Config.Image = "ghcr.io/nokia/srlinux:latest"
	}
	// SR Linux default name for config file is either config.json or config.cli.
	// This depends on the extension of the provided startup-config file.
	if pb.Config.ConfigFile == "" {
		ext := filepath.Ext(pb.Config.GetFile())
		if ext != ".json" {
			ext = ".cli"
		}

		pb.Config.ConfigFile = "config" + ext
	}
	return pb
}

// Implement the resetter for SRL
// Using load factory auto-commit to reset default configs
func (n *Node) ResetCfg(ctx context.Context) error {
	log.Infof("%s resetting config", n.Name())

	err := n.SpawnCLIConn()
	if err != nil {
		return err
	}

	resp, err := n.cliConn.SendConfig(
		configResetCmd,
	)
	if err != nil {
		return err
	}

	if resp.Failed != nil {
		return resp.Failed
	}
	log.Infof("%s - finished resetting config", n.Name())

	return n.cliConn.Close()
}

// SpawnCLIConn spawns a CLI connection towards a Network OS using `kubectl exec` terminal and ensures CLI is ready
// to accept inputs.
// scrapligo options can be provided to this function for a caller to modify scrapligo platform.
// For example, mock transport can be set via options
func (n *Node) SpawnCLIConn() error {
	opts := []scrapliutil.Option{
		scrapliopts.WithAuthBypass(),
		// jacked up terminal width to allow for long strings
		// such as cert and key to not break the terminal
		scrapliopts.WithTermWidth(5000),
	}

	// add options defined in test package
	opts = append(opts, n.testOpts...)

	opts = n.PatchCLIConnOpen("kubectl", []string{"sr_cli", "-d"}, opts)

	var err error
	n.cliConn, err = n.GetCLIConn(scrapliPlatformName, opts)

	if err != nil {
		return err
	}

	return srlinux.WaitSRLMgmtSrvReady(context.TODO(), n.cliConn)
}

// isConfigDataPresent is a helper function that returns true
// if either a string blob or file with config was set in topo file
func (n *Node) isConfigDataPresent() bool {
	if n.GetProto().GetConfig().GetData() != nil || n.GetProto().GetConfig().GetFile() != "" {
		return true
	}

	return false
}

func init() {
	node.Vendor(topopb.Vendor_NOKIA, New)
}
