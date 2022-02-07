package ixia

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	log "github.com/sirupsen/logrus"
	corev1 "k8s.io/api/core/v1"
	errapi "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	tpb "github.com/google/kne/proto/topo"
	"github.com/google/kne/topo/node"
)

type IxiaSpec struct {
	Config  string `json:"config,omitempty"`
	Version string `json:"version,omitempty"`
}

type IxiaStatus struct {
	Status string `json:"status,omitempty"`
	Reason string `json:"reason,omitempty"`
}

type Ixia struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec IxiaSpec `json:"spec,omitempty"`
	// This is a temporary fix until Ixia operator is made public and IxiaTG type can be referenced
	Status IxiaStatus `json:"status,omitempty"`
}

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
}

func (n *Node) Create(ctx context.Context) error {
	log.Infof("Creating IxiaTG node resource %s", n.Name())
	jsonConfig, err := json.Marshal(n.Proto.Config)
	if err != nil {
		return err
	}
	newIxia := &Ixia{
		TypeMeta: metav1.TypeMeta{
			APIVersion: "network.keysight.com/v1alpha1",
			Kind:       "IxiaTG",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      n.Name(),
			Namespace: n.Namespace,
		},
		Spec: IxiaSpec{
			Config:  string(jsonConfig),
			Version: n.Proto.Version,
		},
	}
	body, err := json.Marshal(newIxia)
	if err != nil {
		return err
	}

	err = n.KubeClient.CoreV1().RESTClient().
		Post().
		AbsPath("/apis/network.keysight.com/v1alpha1").
		Namespace(n.Namespace).
		Resource("Ixiatgs").
		Body(body).
		Do(ctx).
		Error()
	if err != nil {
		log.Error(err)
		return err
	}
	log.Infof("Created custom resource: %s", n.Name())
	if err := n.CreateService(ctx); err != nil {
		return err
	}
	return nil
}

func (n *Node) Status(ctx context.Context) (corev1.PodPhase, error) {
	ixiaNode := Ixia{}
	var phase corev1.PodPhase
	var err error
	phase = corev1.PodUnknown
	pod, err := n.KubeClient.CoreV1().Pods(n.Namespace).Get(ctx, n.Name(), metav1.GetOptions{})
	if err != nil {
		// Ignore not found error, because operator may not have picked up immediately
		if !errapi.IsNotFound(err) {
			return phase, err
		}
	}
	if pod != nil {
		phase = pod.Status.Phase
		if phase == corev1.PodRunning {
			return phase, nil
		}
	}

	res := n.KubeClient.CoreV1().RESTClient().
		Get().
		AbsPath("/apis/network.keysight.com/v1alpha1").
		Namespace(n.Namespace).
		Resource("Ixiatgs").
		Name(n.Name()).
		Do(ctx)

	resraw, _ := res.Raw()
	if err = json.Unmarshal(resraw, &ixiaNode); err == nil {
		if ixiaNode.Status.Status == "Failed" {
			return phase, errors.New(ixiaNode.Status.Reason)
		}
	}

	return phase, nil
}

func (n *Node) Delete(ctx context.Context) error {
	log.Infof("Deleting IxiaTG node resource %s", n.Name())
	err := n.KubeClient.CoreV1().RESTClient().
		Delete().
		AbsPath("/apis/network.keysight.com/v1alpha1").
		Namespace(n.Namespace).
		Resource("Ixiatgs").
		Name(n.Name()).
		Do(ctx).
		Error()
	if err != nil {
		log.Error(err)
		return err
	}
	log.Infof("Deleted custom resource: %s", n.Name())
	if err := n.DeleteService(ctx); err != nil {
		return err
	}
	return nil
}

func defaults(pb *tpb.Node) *tpb.Node {
	return pb
}

func init() {
	node.Register(tpb.Node_IXIA_TG, New)
	node.Vendor(tpb.Vendor_KEYSIGHT, New)
}
