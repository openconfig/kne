package srl

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	topopb "github.com/openconfig/kne/proto/topo"
	"github.com/openconfig/kne/topo/node"
	scraplinetwork "github.com/scrapli/scrapligo/driver/network"
	scrapliopts "github.com/scrapli/scrapligo/driver/options"
	scrapliutil "github.com/scrapli/scrapligo/util"
	log "github.com/sirupsen/logrus"
	srlclient "github.com/srl-labs/srl-controller/api/clientset/v1alpha1"
	srltypes "github.com/srl-labs/srl-controller/api/types/v1alpha1"
	srlinux "github.com/srl-labs/srlinux-scrapli"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/fields"
)

const (
	scrapliPlatformName = "nokia_srl"

	readyTimeout = 3 * time.Minute
	retryTimer   = 2 * time.Second

	mgmtServerRdyCmd  = "info from state system app-management application mgmt_server state | grep running"
	commitCompleteCmd = "info from state system configuration commit 1 status | grep complete"
)

// ErrIncompatibleCliConn raised when an invalid scrapligo cli transport type is found.
var ErrIncompatibleCliConn = errors.New("incompatible cli connection in use")

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
	return n, nil
}

type Node struct {
	*node.Impl
	cliConn *scraplinetwork.Driver

	// scrapli options used in testing
	testOpts []scrapliutil.Option
}

// Add validations for interfaces the node provides
var (
	_ node.Certer = (*Node)(nil)
)

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

	defer n.cliConn.Close()

	if err := srlinux.AddSelfSignedServerTLSProfile(n.cliConn, selfSigned.CertName, false); err == nil {
		log.Infof("%s - finshed cert generation", n.Name())
	}

	return err
}

// Create creates a Nokia SR Linux node by interfacing with srl-labs/srl-controller
func (n *Node) Create(ctx context.Context) error {
	log.Infof("Creating Srlinux node resource %s", n.Name())

	if err := n.CreateConfig(ctx); err != nil {
		return fmt.Errorf("node %s failed to create config-map %w", n.Name(), err)
	}
	log.Infof("Created SR Linux node %s configmap", n.Name())

	srl := &srltypes.Srlinux{
		TypeMeta: metav1.TypeMeta{
			Kind:       "Srlinux",
			APIVersion: "kne.srlinux.dev/v1alpha1",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name: n.Name(),
			Labels: map[string]string{
				"app":  n.Name(),
				"topo": n.Namespace,
			},
		},
		Spec: srltypes.SrlinuxSpec{
			NumInterfaces: len(n.GetProto().GetInterfaces()),
			Config: &srltypes.NodeConfig{
				Command:           n.GetProto().GetConfig().GetCommand(),
				Args:              n.GetProto().GetConfig().GetArgs(),
				Image:             n.GetProto().GetConfig().GetImage(),
				Env:               n.GetProto().GetConfig().GetEnv(),
				EntryCommand:      n.GetProto().GetConfig().GetEntryCommand(),
				ConfigPath:        n.GetProto().GetConfig().GetConfigPath(),
				ConfigFile:        n.GetProto().GetConfig().GetConfigFile(),
				ConfigDataPresent: n.isConfigDataPresent(),
				Cert: &srltypes.CertificateCfg{
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

	c, err := srlclient.NewForConfig(n.RestConfig)
	if err != nil {
		return err
	}

	_, err = c.Srlinux(n.Namespace).Create(ctx, srl)
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
	c, err := srlclient.NewForConfig(n.RestConfig)
	if err != nil {
		return err
	}
	err = c.Srlinux(n.Namespace).Delete(ctx, n.Name(), metav1.DeleteOptions{})
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
		pb.Labels = map[string]string{
			"type": topopb.Node_NOKIA_SRL.String(),
		}
	} else {
		if pb.Labels["type"] == "" {
			pb.Labels["type"] = topopb.Node_NOKIA_SRL.String()
		}
	}
	if pb.Config == nil {
		pb.Config = &topopb.Config{}
	}
	if pb.Config.Image == "" {
		pb.Config.Image = "ghcr.io/nokia/srlinux:latest"
	}
	if pb.Config.ConfigFile == "" {
		pb.Config.ConfigFile = "config.json"
	}
	return pb
}

// SpawnCLIConn spawns a CLI connection towards a Network OS using `kubectl exec` terminal and ensures CLI is ready
// to accept inputs.
// scrapligo options can be provided to this function for a caller to modify scrapligo platform.
// For example, mock transport can be set via options
func (n *Node) SpawnCLIConn() error {
	opts := []scrapliutil.Option{
		scrapliopts.WithAuthBypass(),
		scrapliopts.WithAuthNoStrictKey(),
		// jacked up terminal width to allow for long strings
		// such as cert and key to not break the terminal
		scrapliopts.WithTermWidth(5000),
	}

	// add options defined in test package
	opts = append(opts, n.testOpts...)

	n.PatchCLIConnOpen("kubectl", []string{"sr_cli", "-d"}, opts)

	var err error
	n.cliConn, err = n.GetCLIConn(scrapliPlatformName, opts)

	if err != nil {
		return err
	}

	return n.waitSRLMgmtSrvReady(context.TODO(), n.cliConn)
}

// isConfigDataPresent is a helper function that returns true
// if either a string blob or file with config was set in topo file
func (n *Node) isConfigDataPresent() bool {
	if n.GetProto().GetConfig().GetData() != nil || n.GetProto().GetConfig().GetFile() != "" {
		return true
	}

	return false
}

// waitSRLMgmtSrvReady returns when the node boot sequence reaches the stage when it is ready to accept config commands
// returns an error if not ready by readyTimeout.
func (n *Node) waitSRLMgmtSrvReady(ctx context.Context, d *scraplinetwork.Driver) error {
	ctx, cancel := context.WithTimeout(ctx, readyTimeout)
	defer cancel()

	var err error

	for {
		select {
		case <-ctx.Done():
			return fmt.Errorf("timed out waiting for SR Linux node %s to boot: %v", n.Name(), err)
		default:
			// two commands are checked, first if the mgmt_server is running
			resp, err := d.SendCommand(mgmtServerRdyCmd)
			if err != nil || resp.Failed != nil {
				time.Sleep(retryTimer)
				continue
			}

			if !strings.Contains(resp.Result, "running") {
				time.Sleep(retryTimer)
				continue
			}

			// and then if the initial commit completes
			resp, err = d.SendCommand(commitCompleteCmd)
			if err != nil || resp.Failed != nil {
				time.Sleep(retryTimer)
				continue
			}

			if !strings.Contains(resp.Result, "complete") {
				time.Sleep(retryTimer)
				continue
			}

			return nil
		}
	}
}

func init() {
	node.Register(topopb.Node_NOKIA_SRL, New)
	node.Vendor(topopb.Vendor_NOKIA, New)
}
