package srl

import (
	"context"
	"errors"
	"fmt"
	"io"
	"time"

	topopb "github.com/google/kne/proto/topo"
	"github.com/google/kne/topo/node"
	scraplibase "github.com/scrapli/scrapligo/driver/base"
	scraplinetwork "github.com/scrapli/scrapligo/driver/network"
	scraplitransport "github.com/scrapli/scrapligo/transport"
	log "github.com/sirupsen/logrus"
	srlclient "github.com/srl-labs/kne-controller/api/clientset/v1alpha1"
	srltypes "github.com/srl-labs/kne-controller/api/types/v1alpha1"
	srlinux "github.com/srl-labs/srlinux-scrapli"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/proto"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/fields"
)

// ErrIncompatibleCliConn raised when an invalid scrapligo cli transport type is found.
var ErrIncompatibleCliConn = errors.New("incompatible cli connection in use")

func New(pb *topopb.Node) (node.Implementation, error) {
	cfg := defaults(pb)
	proto.Merge(cfg, pb)
	node.FixServices(cfg)
	return &Node{
		pb: cfg,
	}, nil
}

type Node struct {
	pb      *topopb.Node
	cliConn *scraplinetwork.Driver
}

func (n *Node) Proto() *topopb.Node {
	return n.pb
}

func (n *Node) GenerateSelfSigned(ctx context.Context, ni node.Interface) error {
	selfSigned := n.pb.GetConfig().GetCert().GetSelfSigned()
	if selfSigned == nil {
		log.Infof("%s - no cert config", n.pb.Name)
		return nil
	}
	log.Infof("%s - generating self signed certs", n.pb.Name)
	log.Infof("%s - waiting for pod to be running", n.pb.Name)
	w, err := ni.KubeClient().CoreV1().Pods(ni.Namespace()).Watch(ctx, metav1.ListOptions{
		FieldSelector: fields.SelectorFromSet(
			fields.Set{metav1.ObjectNameField: n.pb.Name},
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
	log.Infof("%s - pod running.", n.pb.Name)

	err = n.SpawnCLIConn(ni.Namespace())
	if err != nil {
		return err
	}

	defer n.cliConn.Close()

	err = srlinux.AddSelfSignedServerTLSProfile(n.cliConn, selfSigned.CertName, false)

	if err == nil {
		log.Infof("%s - finshed cert generation", n.pb.Name)
	}

	return err
}

func (n *Node) ConfigPush(ctx context.Context, ns string, r io.Reader) error {
	return status.Errorf(codes.Unimplemented, "Unimplemented")
}

func (n *Node) CreateNodeResource(ctx context.Context, ni node.Interface) error {
	log.Infof("Creating Srlinux node resource %s", n.pb.Name)

	srl := &srltypes.Srlinux{
		TypeMeta: metav1.TypeMeta{
			Kind:       "Srlinux",
			APIVersion: "kne.srlinux.dev/v1alpha1",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name: n.pb.Name,
			Labels: map[string]string{
				"app":  n.pb.Name,
				"topo": ni.Namespace(),
			},
		},
		Spec: srltypes.SrlinuxSpec{
			NumInterfaces: len(ni.Interfaces()),
			Config: &srltypes.NodeConfig{
				Sleep: 0,
			},
		},
	}

	c, err := srlclient.NewForConfig(ni.RESTConfig())
	if err != nil {
		return err
	}

	_, err = c.Srlinux(ni.Namespace()).Create(ctx, srl)
	if err != nil {
		return err
	}

	// wait till srlinux pods are created in the cluster
	w, err := ni.KubeClient().CoreV1().Pods(ni.Namespace()).Watch(ctx, metav1.ListOptions{
		FieldSelector: fields.SelectorFromSet(fields.Set{metav1.ObjectNameField: n.pb.Name}).String(),
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

	log.Infof("Created Srlinux resource: %s", n.pb.Name)

	return nil
}

func (n *Node) DeleteNodeResource(ctx context.Context, ni node.Interface) error {
	log.Infof("Deleting Srlinux node resource %s", n.pb.Name)

	c, err := srlclient.NewForConfig(ni.RESTConfig())
	if err != nil {
		return err
	}

	err = c.Srlinux(ni.Namespace()).Delete(ctx, n.pb.Name, metav1.DeleteOptions{})
	if err != nil {
		return err
	}

	log.Infof("Deleted custom resource: %s", n.pb.Name)

	return nil
}

func defaults(pb *topopb.Node) *topopb.Node {
	return &topopb.Node{
		Constraints: map[string]string{
			"cpu":    "0.5",
			"memory": "1Gi",
		},
		Services: map[uint32]*topopb.Service{
			443: {
				Name:    "ssl",
				Inside:  443,
				Outside: node.GetNextPort(),
			},
			22: {
				Name:     "ssh",
				Inside:   22,
				NodePort: node.GetNextPort(),
			},
			57400: {
				Name:     "gnmi",
				Inside:   57400,
				NodePort: node.GetNextPort(),
			},
		},
		Labels: map[string]string{
			"type": topopb.Node_NOKIA_SRL.String(),
		},
		Config: &topopb.Config{
			Image: "srlinux:latest",
			Command: []string{
				"/tini",
			},
			Args: []string{
				"--",
				"fixuid",
				"-q",
				"/entrypoint.sh",
				"sudo",
				"bash",
				"-c",
				"touch /.dockerenv && /opt/srlinux/bin/sr_linux",
			},
			Env: map[string]string{
				"SRLINUX": "1",
			},
			EntryCommand: fmt.Sprintf("kubectl exec -it %s -- sr_cli", pb.Name),
			ConfigPath:   "/etc/opt/srlinux",
			ConfigFile:   "config.json",
		},
	}
}

// SpawnCLIConn spawns a CLI connection towards a Network OS using `kubectl exec` terminal and ensures CLI is ready
// to accept inputs.
func (n *Node) SpawnCLIConn(ns string) error {
	d, err := srlinux.NewSRLinuxDriver(
		n.pb.Name,
		scraplibase.WithAuthStrictKey(false),
		scraplibase.WithAuthBypass(true),
	)
	if err != nil {
		return err
	}

	// jacked up PtyWidth to allow for long strings such as cert and key to not break the terminal
	d.Transport.BaseTransportArgs.PtyWidth = 5000

	n.cliConn = d

	err = n.PatchCLIConnOpen(ns)
	if err != nil {
		n.cliConn = nil

		return err
	}

	err = n.WaitCLIReady()
	if err != nil {
		return err
	}

	return nil
}

// PatchCLIConnOpen sets the OpenCmd and ExecCmd of system transport to work with `kubectl exec` terminal.
func (n *Node) PatchCLIConnOpen(ns string) error {
	t, ok := n.cliConn.Transport.Impl.(scraplitransport.SystemTransport)
	if !ok {
		return ErrIncompatibleCliConn
	}

	t.SetExecCmd("kubectl")
	t.SetOpenCmd([]string{"exec", "-it", "-n", ns, n.pb.Name, "--", "sr_cli", "-d"})

	return nil
}

// WaitCLIReady attempts to open the transport channel towards a Network OS and perform scrapligo OnOpen actions
// for a given platform. Retries indefinitely till success.
func (n *Node) WaitCLIReady() error {
	transportReady := false
	for !transportReady {
		if err := n.cliConn.Open(); err != nil {
			log.Debugf("%s - Cli not ready - waiting.", n.pb.Name)
			time.Sleep(time.Second * 2)
			continue
		}
		transportReady = true
		log.Debugf("%s - Cli ready.", n.pb.Name)
	}

	// wait till srlinux management server is ready to accept configs
	return srlinux.WaitSRLMgmtSrv(context.TODO(), n.cliConn)
}

func init() {
	node.Register(topopb.Node_NOKIA_SRL, New)
}
