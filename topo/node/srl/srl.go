package srl

import (
	"context"
	"encoding/json"
	"fmt"
	"io"

	topopb "github.com/google/kne/proto/topo"
	"github.com/google/kne/topo/node"
	log "github.com/sirupsen/logrus"
	srl "github.com/srl-labs/kne-controller/api/v1alpha1"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/proto"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

type SrlinuxSpec struct {
	Config string `json:"config,omitempty"`
}

type Srlinux struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec SrlinuxSpec `json:"spec,omitempty"`
}

func New(pb *topopb.Node) (node.Implementation, error) {
	cfg := defaults(pb)
	proto.Merge(cfg, pb)
	node.FixServices(cfg)
	return &Node{
		pb: cfg,
	}, nil
}

type Node struct {
	pb *topopb.Node
}

func (n *Node) Proto() *topopb.Node {
	return n.pb
}

func (n *Node) GenerateSelfSigned(ctx context.Context, ni node.Interface) error {
	return status.Errorf(codes.Unimplemented, "Unimplemented")
}

func (n *Node) ConfigPush(ctx context.Context, ns string, r io.Reader) error {
	return status.Errorf(codes.Unimplemented, "Unimplemented")
}

func (n *Node) CreateNodeResource(ctx context.Context, ni node.Interface) error {
	log.Infof("Creating Srlinux node resource %s", n.pb.Name)

	srl := srl.Srlinux{
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
		Spec: srl.SrlinuxSpec{
			NumInterfaces: len(ni.Interfaces()),
			Config: &srl.NodeConfig{
				Sleep: 0,
			},
		},
	}
	body, err := json.Marshal(srl)
	if err != nil {
		return err
	}

	err = ni.KubeClient().CoreV1().RESTClient().
		Post().
		AbsPath("/apis/kne.srlinux.dev/v1alpha1").
		Namespace(ni.Namespace()).
		Resource("Srlinuxes").
		Body(body).
		Do(ctx).
		Error()
	if err != nil {
		log.Error(err)
		return err
	}
	log.Infof("Created custom resource: %s", n.pb.Name)
	return nil
}

func (n *Node) DeleteNodeResource(ctx context.Context, ni node.Interface) error {
	log.Infof("Deleting Srlinux node resource %s", n.pb.Name)
	err := ni.KubeClient().CoreV1().RESTClient().
		Delete().
		AbsPath("/apis/kne.srlinux.dev/v1alpha1").
		Namespace(ni.Namespace()).
		Resource("Srlinuxes").
		Name(n.pb.Name).
		Do(ctx).
		Error()
	if err != nil {
		log.Error(err)
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

func init() {
	node.Register(topopb.Node_NOKIA_SRL, New)
}
