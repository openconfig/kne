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
	"sort"
	"strings"
	"time"

	"github.com/ghodss/yaml"
	"github.com/golang/protobuf/proto"
	cpb "github.com/google/kne/proto/controller"
	"github.com/google/kne/topo/node"
	"github.com/kr/pretty"
	log "github.com/sirupsen/logrus"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/encoding/protojson"
	"google.golang.org/protobuf/encoding/prototext"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"

	topologyclientv1 "github.com/google/kne/api/clientset/v1beta1"
	topologyv1 "github.com/google/kne/api/types/v1beta1"
	tpb "github.com/google/kne/proto/topo"

	_ "github.com/google/kne/topo/node/ceos"
	_ "github.com/google/kne/topo/node/cisco"
	_ "github.com/google/kne/topo/node/cptx"
	_ "github.com/google/kne/topo/node/gobgp"
	_ "github.com/google/kne/topo/node/host"
	_ "github.com/google/kne/topo/node/ixia"
	_ "github.com/google/kne/topo/node/srl"
)

var protojsonUnmarshaller = protojson.UnmarshalOptions{
	AllowPartial:   true,
	DiscardUnknown: false,
}

// TopologyManager manages a topology.
type TopologyManager interface {
	CheckNodeStatus(context.Context, time.Duration) error
	ConfigPush(context.Context, string, io.Reader) error
	Delete(context.Context) error
	Load(context.Context) error
	Node(string) (node.Node, error)
	Nodes() []node.Node
	TopologySpecs(context.Context) ([]*topologyv1.Topology, error)
	TopologyResources(ctx context.Context) ([]*topologyv1.Topology, error)
	Push(context.Context) error
	Resources(context.Context) (*Resources, error)
	TopologyProto() *tpb.Topology
	Watch(context.Context) error
}

// Manager is a topology instance manager for k8s cluster instance.
type Manager struct {
	BasePath string
	kubecfg  string
	kClient  kubernetes.Interface
	tClient  topologyclientv1.Interface
	rCfg     *rest.Config
	proto    *tpb.Topology
	nodes    map[string]node.Node
}

type Option func(m *Manager)

func WithTopology(t *tpb.Topology) Option {
	return func(m *Manager) {
		m.proto = t
	}
}

func WithKubeClient(c kubernetes.Interface) Option {
	return func(m *Manager) {
		m.kClient = c
	}
}

func WithTopoClient(c topologyclientv1.Interface) Option {
	return func(m *Manager) {
		m.tClient = c
	}
}

func WithClusterConfig(r *rest.Config) Option {
	return func(m *Manager) {
		m.rCfg = r
	}
}

func WithBasePath(s string) Option {
	return func(m *Manager) {
		m.BasePath = s
	}
}

// New creates a new topology manager based on the provided kubecfg and topology.
func New(kubecfg string, pb *tpb.Topology, opts ...Option) (TopologyManager, error) {
	m := &Manager{
		kubecfg: kubecfg,
		proto:   pb,
		nodes:   map[string]node.Node{},
	}
	for _, o := range opts {
		o(m)
	}
	if m.proto == nil {
		return nil, fmt.Errorf("topology protobuf cannot be nil")
	}
	log.Infof("Creating manager for: %s", m.proto.Name)
	if m.rCfg == nil {
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
		m.rCfg = rCfg
	}
	if m.kClient == nil {
		kClient, err := kubernetes.NewForConfig(m.rCfg)
		if err != nil {
			return nil, err
		}
		m.kClient = kClient
	}
	if m.tClient == nil {
		tClient, err := topologyclientv1.NewForConfig(m.rCfg)
		if err != nil {
			return nil, err
		}
		m.tClient = tClient
	}
	return m, nil
}

// Load creates an instance of the managed topology.
func (m *Manager) Load(ctx context.Context) error {
	nMap := map[string]*tpb.Node{}
	for _, n := range m.proto.Nodes {
		if len(n.Interfaces) == 0 {
			n.Interfaces = map[string]*tpb.Interface{}
		} else {
			for k := range n.Interfaces {
				if n.Interfaces[k].IntName == "" {
					n.Interfaces[k].IntName = k
				}
			}
		}
		nMap[n.Name] = n
	}
	uid := 0
	for _, l := range m.proto.Links {
		log.Infof("Adding Link: %s:%s %s:%s", l.ANode, l.AInt, l.ZNode, l.ZInt)
		aNode, ok := nMap[l.ANode]
		if !ok {
			return fmt.Errorf("invalid topology: missing node %q", l.ANode)
		}
		aInt, ok := aNode.Interfaces[l.AInt]
		if !ok {
			aInt = &tpb.Interface{
				IntName: l.AInt,
			}
			aNode.Interfaces[l.AInt] = aInt
		}
		zNode, ok := nMap[l.ZNode]
		if !ok {
			return fmt.Errorf("invalid topology: missing node %q", l.ZNode)
		}
		zInt, ok := zNode.Interfaces[l.ZInt]
		if !ok {
			zInt = &tpb.Interface{
				IntName: l.ZInt,
			}
			zNode.Interfaces[l.ZInt] = zInt
		}
		if aInt.PeerName != "" {
			return fmt.Errorf("interface %s:%s already connected", l.ANode, l.AInt)
		}
		if zInt.PeerName != "" {
			return fmt.Errorf("interface %s:%s already connected", l.ZNode, l.ZInt)
		}
		aInt.PeerName = l.ZNode
		aInt.PeerIntName = l.ZInt
		aInt.Uid = int64(uid)
		zInt.PeerName = l.ANode
		zInt.PeerIntName = l.AInt
		zInt.Uid = int64(uid)
		uid++
	}
	for k, n := range nMap {
		log.Infof("Adding Node: %s:%s:%s", n.Name, n.Vendor, n.Type)
		nn, err := node.New(m.proto.Name, n, m.kClient, m.rCfg, m.BasePath, m.kubecfg)
		if err != nil {
			return fmt.Errorf("failed to load topology: %w", err)
		}
		m.nodes[k] = nn
	}
	return nil
}

// TopologyResources gets the topology CRDs for the cluster.
func (m *Manager) TopologyResources(ctx context.Context) ([]*topologyv1.Topology, error) {
	topology, err := m.tClient.Topology(m.proto.Name).List(ctx, metav1.ListOptions{})
	if err != nil {
		return nil, fmt.Errorf("failed to get topology CRDs: %v", err)
	}

	items := make([]*topologyv1.Topology, len(topology.Items))
	for i := range items {
		items[i] = &topology.Items[i]
	}

	return items, nil
}

// Topology returns the topology protobuf.
func (m *Manager) TopologyProto() *tpb.Topology {
	return m.proto
}

func (m *Manager) TopologySpecs(ctx context.Context) ([]*topologyv1.Topology, error) {
	nodeSpecs := map[string]*[]*topologyv1.Topology{}
	topos := []*topologyv1.Topology{}

	// get topology specs from all nodes
	for _, n := range m.nodes {
		log.Infof("Getting topology specs for node %s", n.Name())
		specs, err := n.TopologySpecs(ctx)
		if err != nil {
			return nil, fmt.Errorf("could not fetch topology specs for node %s: %v", n.Name(), err)
		}

		log.Tracef("Topology specs for node %s: %+q", n.Name(), specs)
		nodeSpecs[n.Name()] = &specs
	}

	// replace node name with pod name, for peer pod attribute in each link
	for nodeName, specs := range nodeSpecs {
		for _, spec := range *specs {
			for l := range spec.Spec.Links {
				link := &spec.Spec.Links[l]
				peerSpecs, ok := nodeSpecs[link.PeerPod]
				if !ok {
					return nil, fmt.Errorf("specs do not exist for node %s", link.PeerPod)
				}

				foundPeer := false
				for _, peerSpec := range *peerSpecs {
					for _, peerLink := range peerSpec.Spec.Links {
						if peerLink.UID == link.UID && peerLink.LocalIntf == link.PeerIntf {
							link.PeerPod = peerSpec.ObjectMeta.Name
							foundPeer = true
							break
						}
					}
					if foundPeer {
						break
					}
				}

				if !foundPeer {
					return nil, fmt.Errorf("could not find peer for node %s pod %s link UID %d", nodeName, spec.ObjectMeta.Name, link.UID)
				}
			}

			topos = append(topos, spec)
		}
	}

	return topos, nil
}

// Push pushes the current topology to k8s.
func (m *Manager) Push(ctx context.Context) error {
	if _, err := m.kClient.CoreV1().Namespaces().Get(ctx, m.proto.Name, metav1.GetOptions{}); err != nil {
		log.Infof("Creating namespace for topology: %q", m.proto.Name)
		ns := &corev1.Namespace{
			ObjectMeta: metav1.ObjectMeta{
				Name: m.proto.Name,
			},
		}
		sNs, err := m.kClient.CoreV1().Namespaces().Create(ctx, ns, metav1.CreateOptions{})
		if err != nil {
			return err
		}
		log.Infof("Server Namespace: %+v", sNs)
	}

	log.Infof("Getting topology specs for namespace %s", m.proto.Name)
	topologies, err := m.TopologySpecs(ctx)
	if err != nil {
		return fmt.Errorf("could not get meshnet topologies: %v", err)
	}
	log.Tracef("Got topology specs for namespace %s: %+q", m.proto.Name, topologies)
	for _, t := range topologies {
		log.Infof("Creating topology for meshnet node %s", t.ObjectMeta.Name)
		sT, err := m.tClient.Topology(m.proto.Name).Create(ctx, t)
		if err != nil {
			return fmt.Errorf("could not create topology for meshnet node %s: %v", t.ObjectMeta.Name, err)
		}
		log.Infof("Meshnet Node:\n%+v\n", sT)
	}

	log.Infof("Creating Node Pods")
	for k, n := range m.nodes {
		if err := n.Create(ctx); err != nil {
			return err
		}
		log.Infof("Node %q resource created", k)
	}
	for _, n := range m.nodes {
		err := GenerateSelfSigned(ctx, n)
		switch {
		default:
			return fmt.Errorf("failed to generate cert for node %s: %w", n.Name(), err)
		case err == nil, status.Code(err) == codes.Unimplemented:
		}
	}
	return nil
}

// CheckNodeStatus reports node status, ignores for unimplemented nodes.
func (m *Manager) CheckNodeStatus(ctx context.Context, timeout time.Duration) error {
	foundAll := false
	processed := make(map[string]bool)

	// Check until end state or timeout sec expired
	start := time.Now()
	for (timeout == 0 || time.Since(start) < timeout) && !foundAll {
		foundAll = true
		for name, n := range m.nodes {
			if _, ok := processed[name]; ok {
				continue
			}

			phase, err := n.Status(ctx)
			if err != nil || phase == node.NODE_FAILED {
				return fmt.Errorf("Node %q: Status %s Reason %v", name, phase, err)
			}
			if phase == node.NODE_RUNNING {
				log.Infof("Node %q: Status %s", name, phase)
				processed[name] = true
			} else {
				foundAll = false
			}
		}
		time.Sleep(100 * time.Millisecond)
	}
	if !foundAll {
		log.Warnf("Failed to determine status of some node resources in %d sec", timeout)
	}
	return nil
}

// GenerateSelfSigned will try to create self signed certs on the provided node. If the node
// doesn't have cert info then it is a noop. If the node doesn't fulfil Certer then
// status.Unimplmented will be returned.
func GenerateSelfSigned(ctx context.Context, n node.Node) error {
	if n.GetProto().GetConfig().GetCert() == nil {
		log.Debugf("No cert info for %s", n.Name())
		return nil
	}
	nCert, ok := n.(node.Certer)
	if !ok {
		return status.Errorf(codes.Unimplemented, "node %s does not implement Certer interface", n.Name())
	}
	return nCert.GenerateSelfSigned(ctx)
}

// Delete deletes the topology from k8s.
func (m *Manager) Delete(ctx context.Context) error {
	if _, err := m.kClient.CoreV1().Namespaces().Get(ctx, m.proto.Name, metav1.GetOptions{}); err != nil {
		return fmt.Errorf("topology %q does not exist in cluster", m.proto.Name)
	}

	// Delete topology nodes
	for _, n := range m.nodes {
		// Delete Service for node
		if err := n.Delete(ctx); err != nil {
			log.Warnf("Error deleting node %q: %v", n.Name(), err)
		}
	}
	// Delete meshnet nodes
	nodes, err := m.TopologyResources(ctx)
	if err == nil {
		for _, n := range nodes {
			if err := m.tClient.Topology(m.proto.Name).Delete(ctx, n.ObjectMeta.Name, metav1.DeleteOptions{}); err != nil {
				log.Warnf("Error meshnet node %q: %v", n.ObjectMeta.Name, err)
			}
		}
	} else {
		// no need to return warning as deleting meshnet namespace shall delete the resources too
		log.Warnf("Error getting meshnet nodes: %v", err)
	}

	// Delete namespace
	prop := metav1.DeletePropagationForeground
	if err := m.kClient.CoreV1().Namespaces().Delete(ctx, m.proto.Name, metav1.DeleteOptions{
		PropagationPolicy: &prop,
	}); err != nil {
		return err
	}
	return nil
}

// Load loads a Topology from fName.
func Load(fName string) (*tpb.Topology, error) {
	b, err := ioutil.ReadFile(fName)
	if err != nil {
		return nil, err
	}
	t := &tpb.Topology{}

	if strings.HasSuffix(fName, ".yaml") || strings.HasSuffix(fName, ".yml") {
		jsonBytes, err := yaml.YAMLToJSON(b)
		if err != nil {
			return nil, fmt.Errorf("could not parse yaml: %v", err)
		}
		if err := protojsonUnmarshaller.Unmarshal(jsonBytes, t); err != nil {
			return nil, fmt.Errorf("could not parse json: %v", err)
		}
	} else {
		if err := prototext.Unmarshal(b, t); err != nil {
			return nil, err
		}
	}

	return t, nil
}

type Resources struct {
	Services   map[string][]*corev1.Service
	Pods       map[string][]*corev1.Pod
	ConfigMaps map[string]*corev1.ConfigMap
	Topologies map[string]*topologyv1.Topology
}

// Nodes returns all nodes in the current topology.
func (m *Manager) Nodes() []node.Node {
	var keys []string
	for k := range m.nodes {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	var n []node.Node
	for _, k := range keys {
		n = append(n, m.nodes[k])
	}
	return n
}

// Resources gets the currently configured resources from the topology.
func (m *Manager) Resources(ctx context.Context) (*Resources, error) {
	r := Resources{
		Services:   map[string][]*corev1.Service{},
		Pods:       map[string][]*corev1.Pod{},
		ConfigMaps: map[string]*corev1.ConfigMap{},
		Topologies: map[string]*topologyv1.Topology{},
	}

	for nodeName, n := range m.nodes {
		pods, err := n.Pods(ctx)
		if err != nil {
			return nil, fmt.Errorf("could not get pods for node %s: %v", nodeName, err)
		}
		r.Pods[nodeName] = pods

		services, err := n.Services(ctx)
		if err != nil {
			return nil, fmt.Errorf("could not get services for node %s: %v", nodeName, err)
		}
		r.Services[nodeName] = services
	}

	tList, err := m.TopologyResources(ctx)
	if err != nil {
		return nil, err
	}
	for _, t := range tList {
		r.Topologies[t.Name] = t
	}

	return &r, nil
}

func (m *Manager) Watch(ctx context.Context) error {
	watcher, err := m.tClient.Topology(m.proto.Name).Watch(ctx, metav1.ListOptions{})
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
	cp, ok := d.(node.ConfigPusher)
	if !ok {
		return nil
	}
	return cp.ConfigPush(ctx, r)
}

func (m *Manager) Node(nodeName string) (node.Node, error) {
	n, ok := m.nodes[nodeName]
	if !ok {
		return nil, fmt.Errorf("node %q not found", nodeName)
	}
	return n, nil
}

// TopologyParams specifies the parameters used by the functions that
// creates/deletes/show topology.
type TopologyParams struct {
	TopoName       string   // the filename of the topology
	Kubecfg        string   // the path of kube config
	TopoNewOptions []Option // the options used in the TopoNewFunc
	Timeout        time.Duration
	DryRun         bool
}

// CreateTopology creates the topology and configs it.
func CreateTopology(ctx context.Context, params TopologyParams) error {
	var topopb *tpb.Topology
	var err error
	if params.TopoName != "" {
		topopb, err = Load(params.TopoName)
		if err != nil {
			return fmt.Errorf("failed to load %s: %+v", params.TopoName, err)
		}
	}
	t, err := New(params.Kubecfg, topopb, params.TopoNewOptions...)
	if err != nil {
		return fmt.Errorf("failed to create topology for %s: %+v", params.TopoName, err)
	}
	log.Infof("Topology:\n%s\n", proto.MarshalTextString(t.TopologyProto()))
	if err := t.Load(ctx); err != nil {
		return fmt.Errorf("failed to load topology: %w", err)
	}
	if params.DryRun {
		return nil
	}

	if err := t.Push(ctx); err != nil {
		return err
	}
	if err := t.CheckNodeStatus(ctx, params.Timeout); err != nil {
		return err
	}
	log.Infof("Topology %q created\n", t.TopologyProto().GetName())
	r, err := t.Resources(ctx)
	if err != nil {
		return fmt.Errorf("failed to check resource %s: %+v", params.TopoName, err)
	}
	log.Infof("Pods:")
	for _, pods := range r.Pods {
		for _, pod := range pods {
			log.Infof("%v\n", pod.Name)
		}
	}

	return nil
}

// DeleteTopology deletes the topology.
func DeleteTopology(ctx context.Context, params TopologyParams) error {
	var topopb *tpb.Topology
	var err error
	if params.TopoName != "" {
		topopb, err = Load(params.TopoName)
		if err != nil {
			return fmt.Errorf("failed to load %s: %+v", params.TopoName, err)
		}
	}
	t, err := New(params.Kubecfg, topopb, params.TopoNewOptions...)
	if err != nil {
		return fmt.Errorf("failed to delete topology for %s: %+v", params.TopoName, err)
	}
	log.Infof("Topology:\n%+v\n", proto.MarshalTextString(t.TopologyProto()))
	if err := t.Load(ctx); err != nil {
		return fmt.Errorf("failed to load %s: %+v", params.TopoName, err)
	}

	if err := t.Delete(ctx); err != nil {
		return fmt.Errorf("failed to delete %s: %+v", params.TopoName, err)
	}
	log.Infof("Successfully deleted topology: %q", t.TopologyProto().GetName())

	return nil
}

// serviceToProto creates the service mapping.
func serviceToProto(s *corev1.Service, m map[uint32]*tpb.Service) error {
	if s == nil || m == nil {
		return fmt.Errorf("service and map must not be nil")
	}
	if len(s.Status.LoadBalancer.Ingress) == 0 {
		return fmt.Errorf("service %s has no external loadbalancer configured", s.Name)
	}
	for _, p := range s.Spec.Ports {
		k := uint32(p.Port)
		service, ok := m[k]
		if !ok {
			service = &tpb.Service{
				Name:   p.Name,
				Inside: k,
			}
			m[k] = service
		}
		if service.Name == "" {
			service.Name = p.Name
		}
		service.Outside = uint32(p.TargetPort.IntVal)
		service.NodePort = uint32(p.NodePort)
		service.InsideIp = s.Spec.ClusterIP
		service.OutsideIp = s.Status.LoadBalancer.Ingress[0].IP
	}
	return nil
}

var (
	new = New // a non-public new to allow overriding the New() in this package.
)

// sMap keeps the POD state of all topology nodes.
type sMap struct {
	m map[string]node.NodeStatus
}

func (s *sMap) Size() int {
	return len(s.m)
}

func (s *sMap) SetNodeState(name string, state node.NodeStatus) {
	if s.m == nil {
		s.m = map[string]node.NodeStatus{}
	}
	s.m[name] = state
}

func (s *sMap) TopoState() cpb.TopologyState {
	if s == nil || len(s.m) == 0 {
		return cpb.TopologyState_TOPOLOGY_STATE_UNKNOWN
	}
	cntTable := map[node.NodeStatus]int{}
	for _, gotState := range s.m {
		cntTable[gotState]++
	}

	if cntTable[node.NODE_RUNNING] == s.Size() {
		return cpb.TopologyState_TOPOLOGY_STATE_RUNNING
	}
	if cntTable[node.NODE_FAILED] > 0 {
		return cpb.TopologyState_TOPOLOGY_STATE_ERROR
	}
	if cntTable[node.NODE_PENDING] > 0 {
		return cpb.TopologyState_TOPOLOGY_STATE_CREATING
	}
	return cpb.TopologyState_TOPOLOGY_STATE_UNKNOWN
}

// GetTopologyServices returns the topology information.
func GetTopologyServices(ctx context.Context, params TopologyParams) (*cpb.ShowTopologyResponse, error) {
	var topopb *tpb.Topology
	var err error
	if params.TopoName != "" {
		topopb, err = Load(params.TopoName)
		if err != nil {
			return nil, fmt.Errorf("failed to load %s: %+v", params.TopoName, err)
		}
	}

	t, err := new(params.Kubecfg, topopb, params.TopoNewOptions...)
	if err != nil {
		return nil, fmt.Errorf("failed to get topology service for %s: %+v", params.TopoName, err)
	}

	if err := t.Load(ctx); err != nil {
		return nil, fmt.Errorf("failed to load %s: %+v", params.TopoName, err)
	}

	r, err := t.Resources(ctx)
	if err != nil {
		return nil, err
	}
	for _, n := range t.TopologyProto().Nodes {
		if len(n.Services) == 0 {
			n.Services = map[uint32]*tpb.Service{}
		}

		services, ok := r.Services[n.Name]
		if !ok {
			continue
		}

		for _, svc := range services {
			serviceToProto(svc, n.Services)
		}
	}
	sMap := &sMap{}
	for _, n := range t.Nodes() {
		phase, _ := n.Status(ctx)
		sMap.SetNodeState(n.Name(), phase)
	}
	return &cpb.ShowTopologyResponse{
		State:    sMap.TopoState(),
		Topology: t.TopologyProto(),
	}, nil
}
