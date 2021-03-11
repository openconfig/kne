// Copyright 2021 Google LLC
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//      http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package topo

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"io/ioutil"
	"strings"

	log "github.com/sirupsen/logrus"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/util/intstr"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/kubernetes/scheme"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"
	"k8s.io/client-go/tools/remotecommand"

	"github.com/golang/protobuf/proto"
	topologyclientv1 "github.com/hfam/kne/api/clientset/v1beta1"
	topologyv1 "github.com/hfam/kne/api/types/v1beta1"
	topopb "github.com/hfam/kne/proto/topo"
)

var (
	meshNetCRD = map[string]string{
		"group":   "networkop.co.uk",
		"version": "v1beta1",
		"plural":  "topologies",
	}
)

// Manager is a topology instance manager for k8s cluster instance.
type Manager struct {
	kClient kubernetes.Interface
	tClient topologyclientv1.Interface
	rCfg    *rest.Config
	tpb     *topopb.Topology
	nodes   map[string]*Node
	links   map[string]*Link
}

// New creates a new topology manager based on the provided kubecfg and topology.
func New(kubecfg string, tpb *topopb.Topology) (*Manager, error) {
	log.Infof("Creating manager for: %s", tpb.Name)
	// use the current context in kubeconfig try in-cluster first if not fallback to kubeconfig
	log.Infof("Trying in-cluster configuration")
	rCfg, err := rest.InClusterConfig()
	if err != nil {
		log.Infof("Falling back to kubeconfig: %q", kubecfg)
		rCfg, err = clientcmd.BuildConfigFromFlags("", kubecfg)
		if err != nil {
			return nil, err
		}
	}
	// create the clientset
	kClient, err := kubernetes.NewForConfig(rCfg)
	if err != nil {
		return nil, err
	}
	tClient, err := topologyclientv1.NewForConfig(rCfg)
	if err != nil {
		return nil, err
	}

	return &Manager{
		kClient: kClient,
		tClient: tClient,
		rCfg:    rCfg,
		tpb:     tpb,
		nodes:   map[string]*Node{},
		links:   map[string]*Link{},
	}, nil
}

// Load creates an instance of the managed topology.
func (m *Manager) Load(ctx context.Context) error {
	for _, n := range m.tpb.Nodes {
		log.Infof("Adding Node: %s:%s", n.Name, n.Type)
		m.nodes[n.Name] = NewNode(m.tpb.Name, n, m.kClient, m.rCfg)
	}
	uid := 0
	for _, l := range m.tpb.Links {
		log.Infof("Adding Link: %s:%s %s:%s", l.ANode, l.AInt, l.ZNode, l.ZInt)
		sNode, ok := m.nodes[l.ANode]
		if !ok {
			return fmt.Errorf("invalid topology: missing node %q", l.ANode)
		}
		dNode, ok := m.nodes[l.ZNode]
		if !ok {
			return fmt.Errorf("invalid topology: missing node %q", l.ZNode)
		}
		if _, ok := sNode.interfaces[l.AInt]; ok {
			return fmt.Errorf("interface %s:%s already connected", l.ANode, l.AInt)
		}
		if _, ok := dNode.interfaces[l.ZInt]; ok {
			return fmt.Errorf("interface %s:%s already connected", l.ZNode, l.ZInt)
		}
		link := &Link{
			uid: uid,
			pb:  l,
		}
		sNode.interfaces[l.AInt] = link
		dl := proto.Clone(l).(*topopb.Link)
		dl.AInt, dl.ZInt = dl.ZInt, dl.AInt
		dl.ANode, dl.ZNode = dl.ZNode, dl.ANode
		dLink := &Link{
			pb: dl,
		}
		dNode.interfaces[l.ZInt] = dLink
		uid++
	}
	return nil
}

// Pods gets all pods in the managed k8s cluster.
func (m *Manager) Pods(ctx context.Context) error {
	pods, err := m.kClient.CoreV1().Pods(m.tpb.Name).List(ctx, metav1.ListOptions{})
	if err != nil {
		return err
	}
	for _, p := range pods.Items {
		fmt.Println(p.Namespace, p.Name)
	}
	return nil
}

// Topology gets the topology CRDs for the cluster.
func (m *Manager) Topology(ctx context.Context) error {
	topology, err := m.tClient.Topology(m.tpb.Name).List(ctx, metav1.ListOptions{})
	if err != nil {
		return fmt.Errorf("failed to get topology CRDs: %v", err)
	}
	for _, t := range topology.Items {
		fmt.Printf("%+v\n", t)
	}
	return nil
}

// Push pushes the current topology to k8s.
func (m *Manager) Push(ctx context.Context) error {
	if _, err := m.kClient.CoreV1().Namespaces().Get(ctx, m.tpb.Name, metav1.GetOptions{}); err != nil {
		log.Infof("Creating namespace for topology: %q", m.tpb.Name)
		ns := &corev1.Namespace{
			ObjectMeta: metav1.ObjectMeta{
				Name: m.tpb.Name,
			},
		}
		sNs, err := m.kClient.CoreV1().Namespaces().Create(ctx, ns, metav1.CreateOptions{})
		if err != nil {
			return err
		}
		log.Infof("Server Namespace: %+v", sNs)
	}

	log.Infof("Pushing Topology to k8s: %q", m.tpb.Name)
	for _, n := range m.nodes {
		t := &topologyv1.Topology{
			ObjectMeta: metav1.ObjectMeta{
				Name: n.pb.Name,
			},
			Spec: topologyv1.TopologySpec{},
		}
		var links []topologyv1.Link
		for _, intf := range n.interfaces {
			link := topologyv1.Link{
				LocalIntf: intf.pb.AInt,
				LocalIP:   "",
				PeerIntf:  intf.pb.ZInt,
				PeerIP:    "",
				PeerPod:   intf.pb.ZNode,
				UID:       intf.uid,
			}
			links = append(links, link)
		}
		t.Spec.Links = links
		sT, err := m.tClient.Topology(m.tpb.Name).Create(ctx, t)
		if err != nil {
			return err
		}
		log.Infof("Topology:\n%+v\n", sT)
	}
	return nil
}

// Delete deletes the topology from k8s.
func (m *Manager) Delete(ctx context.Context) error {
	if _, err := m.kClient.CoreV1().Namespaces().Get(ctx, m.tpb.Name, metav1.GetOptions{}); err != nil {
		return fmt.Errorf("topology %q does not exist in cluster", m.tpb.Name)
	}
	prop := metav1.DeletePropagationForeground
	if err := m.kClient.CoreV1().Namespaces().Delete(ctx, m.tpb.Name, metav1.DeleteOptions{
		PropagationPolicy: &prop,
	}); err != nil {
		return err
	}
	return nil
}

// Load loads a Topology from fName.
func Load(fName string) (*topopb.Topology, error) {
	b, err := ioutil.ReadFile(fName)
	if err != nil {
		return nil, err
	}
	t := &topopb.Topology{}
	if err := proto.UnmarshalText(string(b), t); err != nil {
		return nil, err
	}
	return t, nil
}

// Config is the per node specific configuration.
type Config struct {
	command      []string
	args         []string
	image        string
	env          []string
	sleep        int
	entryCmd     string
	cfgPath      string
	bootFileName string
	bootFile     string
	inside       int32
	outside      int32
}

// Node is a topology node in the cluster.
type Node struct {
	id         int
	namespace  string
	pb         *topopb.Node
	kClient    kubernetes.Interface
	rCfg       *rest.Config
	cfg        Config
	interfaces map[string]*Link
}

// NewNode creates a new node for use in the k8s cluster.  Configure will push the node to
// the cluster.
func NewNode(namespace string, pb *topopb.Node, kClient kubernetes.Interface, rCfg *rest.Config) *Node {
	return &Node{
		namespace:  namespace,
		pb:         pb,
		rCfg:       rCfg,
		kClient:    kClient,
		interfaces: map[string]*Link{},
	}
}

// Configure creates the node on the k8s cluster.
func (n *Node) Configure(ctx context.Context) error {
	cm := &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Name: fmt.Sprintf("%s-config", n.pb.Name),
		},
		Data: map[string]string{
			n.cfg.bootFileName: n.cfg.bootFile,
		},
	}
	sCM, err := n.kClient.CoreV1().ConfigMaps(n.namespace).Create(ctx, cm, metav1.CreateOptions{})
	if err != nil {
		return err
	}
	log.Infof("Server Config Map:\n%v\n", sCM)
	return nil
}

// Delete removes the Node from the cluster.
func (n *Node) Delete(ctx context.Context) error {
	return n.kClient.CoreV1().ConfigMaps(n.namespace).Delete(ctx, fmt.Sprintf("%s-config", n.pb.Name), metav1.DeleteOptions{})
}

// CreateService add the service definition for the Node.
func (n *Node) CreateService(ctx context.Context) error {
	s := &corev1.Service{
		TypeMeta: metav1.TypeMeta{
			Kind:       "Service",
			APIVersion: "v1",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name: fmt.Sprintf("service-%s", n.pb.Name),
		},
		Spec: corev1.ServiceSpec{
			Ports: []corev1.ServicePort{{
				Name:       fmt.Sprintf("port-%d", n.cfg.outside),
				Protocol:   "TCP",
				Port:       n.cfg.inside,
				TargetPort: intstr.FromInt(int(n.cfg.inside)),
				NodePort:   n.cfg.outside,
			}},
			Selector: map[string]string{
				"app": n.pb.Name,
			},
			Type: "NodePort",
		},
	}
	sS, err := n.kClient.CoreV1().Services(n.namespace).Create(ctx, s, metav1.CreateOptions{})
	if err != nil {
		return err
	}
	log.Infof("Created Service:\n%v\n", sS)
	return nil
}

// DeleteService removes the service definition for the Node.
func (n *Node) DeleteService(ctx context.Context) error {
	i := int64(0)
	return n.kClient.CoreV1().Services(n.namespace).Delete(ctx, fmt.Sprintf("service-%s", n.pb.Name), metav1.DeleteOptions{
		TypeMeta: metav1.TypeMeta{
			APIVersion: "v1",
		},
		GracePeriodSeconds: &i,
	})
}

// Exec will make a connection via spdy transport to the Pod and execute the provided command.
// It will wire up stdin, stdout, stderr to provided io channels.
func (n *Node) Exec(ctx context.Context, cmd []string, stdin io.Reader, stdout io.Writer, stderr io.Writer) error {
	req := n.kClient.CoreV1().RESTClient().Post().Resource("pods").Name(n.pb.Name).Namespace(n.namespace).SubResource("exec")
	opts := &corev1.PodExecOptions{
		Command: cmd,
		Stdin:   true,
		Stdout:  true,
		Stderr:  true,
		TTY:     true,
	}
	if stdin == nil {
		opts.Stdin = false
	}
	req.VersionedParams(
		opts,
		scheme.ParameterCodec,
	)

	exec, err := remotecommand.NewSPDYExecutor(n.rCfg, "POST", req.URL())
	if err != nil {
		return err
	}
	return exec.Stream(remotecommand.StreamOptions{
		Stdin:  stdin,
		Stdout: stdout,
		Stderr: stderr,
	})
}

// Status returns the current pod state for Node.
func (n *Node) Status(ctx context.Context) (corev1.PodPhase, error) {
	p, err := n.Pod(ctx)
	if err != nil {
		return corev1.PodUnknown, err
	}
	return p.Status.Phase, nil
}

// Pod returns the pod definition for the node.
func (n *Node) Pod(ctx context.Context) (*corev1.Pod, error) {
	return n.kClient.CoreV1().Pods(n.namespace).Get(ctx, n.pb.Name, metav1.GetOptions{})
}

var (
	remountSys = []string{
		"bin/sh",
		"-c",
		"mount -o ro,remount /sys; mount -o rw,remount /sys",
	}
	getBridge = []string{
		"bin/sh",
		"-c",
		"ls /sys/class/net/ | grep br-",
	}
	enableIPForwarding = []string{
		"bin/sh",
		"-c",
		"sysctl -w net.ipv4.ip_forward=1",
	}
)

func enableLLDP(b string) []string {
	return []string{
		"bin/sh",
		"-c",
		fmt.Sprintf("echo 16384 > /sys/class/net/%s/bridge/group_fwd_mask", b),
	}
}

// EnableLLDP enables LLDP on the pod.
func (n *Node) EnableLLDP(ctx context.Context) error {
	log.Infof("Enabling LLDP on node: %s", n.pb.Name)
	stdout := bytes.NewBuffer([]byte{})
	stderr := bytes.NewBuffer([]byte{})
	if err := n.Exec(ctx, remountSys, nil, stdout, stderr); err != nil {
		return err
	}
	log.Infof("stdout:\n%s\nstderr:\n%s", stdout.String(), stderr.String())
	stdout.Reset()
	stderr.Reset()
	if err := n.Exec(ctx, getBridge, nil, stdout, stderr); err != nil {
		return err
	}
	bridges := strings.Split(stdout.String(), "\n")
	for _, b := range bridges {
		stdout.Reset()
		stderr.Reset()
		cmd := enableLLDP(b)
		if err := n.Exec(ctx, cmd, nil, stdout, stderr); err != nil {
			return err
		}
		log.Infof("stdout:\n%s\nstderr:\n%s", stdout.String(), stderr.String())
	}
	return nil
}

// EnableIPForwarding enables IP forwarding on the pod.
func (n *Node) EnableIPForwarding(ctx context.Context) error {
	log.Infof("Enabling IP forwarding for node: %s", n.pb.Name)
	stdout := bytes.NewBuffer([]byte{})
	stderr := bytes.NewBuffer([]byte{})
	if err := n.Exec(ctx, enableIPForwarding, nil, stdout, stderr); err != nil {
		return err
	}
	log.Infof("stdout:\n%s\nstderr:\n%s", stdout.String(), stderr.String())
	return nil
}

var (
	gvd = schema.GroupVersionResource{
		Group:    "networkop.co.uk",
		Resource: "topology",
		Version:  "v1beta1",
	}
)

type Link struct {
	uid int
	pb  *topopb.Link
}
