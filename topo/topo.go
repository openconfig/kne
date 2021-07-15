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
	"context"
	"fmt"
	"io"
	"io/ioutil"
	"sync"

	"github.com/kr/pretty"
	log "github.com/sirupsen/logrus"
	"google.golang.org/protobuf/encoding/prototext"
	"google.golang.org/protobuf/proto"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"

	topologyclientv1 "github.com/google/kne/api/clientset/v1beta1"
	topologyv1 "github.com/google/kne/api/types/v1beta1"
	topopb "github.com/google/kne/proto/topo"
	"github.com/google/kne/topo/node"

	_ "github.com/google/kne/topo/node/ceos"
	_ "github.com/google/kne/topo/node/csr"
	_ "github.com/google/kne/topo/node/cxr"
	_ "github.com/google/kne/topo/node/frr"
	_ "github.com/google/kne/topo/node/host"
	_ "github.com/google/kne/topo/node/ixia"
	_ "github.com/google/kne/topo/node/quagga"
	_ "github.com/google/kne/topo/node/vmx"
)

// Manager is a topology instance manager for k8s cluster instance.
type Manager struct {
	kClient kubernetes.Interface
	tClient topologyclientv1.Interface
	rCfg    *rest.Config
	tpb     *topopb.Topology
	nodes   map[string]*node.Node
	links   map[string]*node.Link
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
		nodes:   map[string]*node.Node{},
		links:   map[string]*node.Link{},
	}, nil
}

// Load creates an instance of the managed topology.
func (m *Manager) Load(ctx context.Context) error {
	for _, n := range m.tpb.Nodes {
		log.Infof("Adding Node: %s:%s", n.Name, n.Type)
		nn, err := node.New(m.tpb.Name, n, m.kClient, m.rCfg)
		if err != nil {
			return fmt.Errorf("failed to load topology: %w", err)
		}
		m.nodes[n.Name] = nn
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
		if _, ok := sNode.Interfaces()[l.AInt]; ok {
			return fmt.Errorf("interface %s:%s already connected", l.ANode, l.AInt)
		}
		if _, ok := dNode.Interfaces()[l.ZInt]; ok {
			return fmt.Errorf("interface %s:%s already connected", l.ZNode, l.ZInt)
		}
		link := &node.Link{
			UID:   uid,
			Proto: l,
		}
		sNode.Interfaces()[l.AInt] = link
		dl := proto.Clone(l).(*topopb.Link)
		dl.AInt, dl.ZInt = dl.ZInt, dl.AInt
		dl.ANode, dl.ZNode = dl.ZNode, dl.ANode
		dLink := &node.Link{
			UID:   uid,
			Proto: dl,
		}
		dNode.Interfaces()[l.ZInt] = dLink
		uid++
	}
	return nil
}

// Topology gets the topology CRDs for the cluster.
func (m *Manager) Topology(ctx context.Context) ([]topologyv1.Topology, error) {
	topology, err := m.tClient.Topology(m.tpb.Name).List(ctx, metav1.ListOptions{})
	if err != nil {
		return nil, fmt.Errorf("failed to get topology CRDs: %v", err)
	}
	return topology.Items, nil
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
				Name: n.Name(),
			},
			Spec: topologyv1.TopologySpec{},
		}
		var links []topologyv1.Link
		for _, intf := range n.Interfaces() {
			link := topologyv1.Link{
				LocalIntf: intf.Proto.AInt,
				LocalIP:   "",
				PeerIntf:  intf.Proto.ZInt,
				PeerIP:    "",
				PeerPod:   intf.Proto.ZNode,
				UID:       intf.UID,
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
	log.Infof("Creating Node Pods")
	for k, n := range m.nodes {
		if err := n.Configure(ctx); err != nil {
			return err
		}
		if err := n.CreateService(ctx); err != nil {
			return err
		}
		if err := n.CreateResource(ctx); err != nil {
			return err
		}
		log.Infof("Node %q resource created", k)
	}
	return nil
}

// Delete deletes the topology from k8s.
func (m *Manager) Delete(ctx context.Context) error {
	if _, err := m.kClient.CoreV1().Namespaces().Get(ctx, m.tpb.Name, metav1.GetOptions{}); err != nil {
		return fmt.Errorf("topology %q does not exist in cluster", m.tpb.Name)
	}

	// Delete topology pods
	for _, n := range m.nodes {
		// Delete Service for node
		if err := n.DeleteService(ctx); err != nil {
			log.Warnf("Error deleting service %q: %v", n.Name(), err)
		}
		// Delete config maps for node
		if err := n.Delete(ctx); err != nil {
			log.Warnf("Error deleting configmaps %q: %v", n.Name(), err)
		}
		// Delete Resource for node
		if err := n.DeleteResource(ctx); err != nil {
			log.Warnf("Error deleting resource %q: %v", n.Name(), err)
		}
		// Delete Topology for node
		if err := m.tClient.Topology(m.tpb.Name).Delete(ctx, n.Name(), metav1.DeleteOptions{}); err != nil {
			log.Warnf("Error deleting topology %q: %v", n.Name(), err)
		}
	}
	// Delete namespace
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
	if err := prototext.Unmarshal(b, t); err != nil {
		return nil, err
	}
	return t, nil
}

type Resources struct {
	Services   map[string]*corev1.Service
	Pods       map[string]*corev1.Pod
	ConfigMaps map[string]*corev1.ConfigMap
	Topologies map[string]*topologyv1.Topology
}

// Resources gets the currently configured resources from the topology.
func (m *Manager) Resources(ctx context.Context) (*Resources, error) {
	r := Resources{
		Services:   map[string]*corev1.Service{},
		Pods:       map[string]*corev1.Pod{},
		ConfigMaps: map[string]*corev1.ConfigMap{},
		Topologies: map[string]*topologyv1.Topology{},
	}
	for _, n := range m.nodes {
		p, err := n.Pod(ctx)
		if err != nil {
			return nil, err
		}
		r.Pods[p.Name] = p
	}
	tList, err := m.Topology(ctx)
	if err != nil {
		return nil, err
	}
	for _, t := range tList {
		v := t
		r.Topologies[v.Name] = &v
	}
	sList, err := m.kClient.CoreV1().Services(m.tpb.Name).List(ctx, metav1.ListOptions{})
	if err != nil {
		return nil, err
	}
	for _, s := range sList.Items {
		sLocal := s
		r.Services[sLocal.Name] = &sLocal
	}
	return &r, nil
}

func (m *Manager) Watch(ctx context.Context) error {
	watcher, err := m.tClient.Topology(m.tpb.Name).Watch(ctx, metav1.ListOptions{})
	if err != nil {
		return err
	}
	ch := watcher.ResultChan()
	for e := range ch {
		fmt.Println(e.Type)
		pretty.Print(e.Object)
		fmt.Println("")
	}
	return nil
}

func (m *Manager) ConfigPush(ctx context.Context, deviceName string, r io.Reader) error {
	d, ok := m.nodes[deviceName]
	if !ok {
		return fmt.Errorf("node %q not found", deviceName)
	}
	return d.ConfigPush(ctx, r)
}

func (m *Manager) EnableIPForwarding(ctx context.Context) error {
	for _, d := range m.nodes {
		if err := d.EnableIPForwarding(ctx); err != nil {
			return err
		}
	}
	return nil
}

func (m *Manager) EnableLLDP(ctx context.Context) error {
	for _, d := range m.nodes {
		if err := d.EnableLLDP(ctx); err != nil {
			return err
		}
	}
	return nil
}

var (
	muPort   sync.Mutex
	nextPort uint32 = 30001
)

func GetNextPort() uint32 {
	muPort.Lock()
	p := nextPort
	nextPort++
	muPort.Unlock()
	return p
}
