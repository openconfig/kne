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
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/ghodss/yaml"
	"github.com/kr/pretty"
	topologyclientv1 "github.com/networkop/meshnet-cni/api/clientset/v1beta1"
	topologyv1 "github.com/networkop/meshnet-cni/api/types/v1beta1"
	"github.com/openconfig/gnmi/errlist"
	"github.com/openconfig/kne/cluster/kind"
	"github.com/openconfig/kne/events"
	"github.com/openconfig/kne/exec/run"
	"github.com/openconfig/kne/metrics"
	"github.com/openconfig/kne/pods"
	cpb "github.com/openconfig/kne/proto/controller"
	epb "github.com/openconfig/kne/proto/event"
	tpb "github.com/openconfig/kne/proto/topo"
	"github.com/openconfig/kne/topo/node"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/encoding/protojson"
	"google.golang.org/protobuf/encoding/prototext"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/watch"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"
	"k8s.io/client-go/util/homedir"
	log "k8s.io/klog/v2"

	_ "github.com/openconfig/kne/topo/node/alpine"
	_ "github.com/openconfig/kne/topo/node/arista"
	_ "github.com/openconfig/kne/topo/node/cisco"
	_ "github.com/openconfig/kne/topo/node/drivenets"
	_ "github.com/openconfig/kne/topo/node/forward"
	_ "github.com/openconfig/kne/topo/node/gobgp"
	_ "github.com/openconfig/kne/topo/node/host"
	_ "github.com/openconfig/kne/topo/node/juniper"
	_ "github.com/openconfig/kne/topo/node/keysight"
	_ "github.com/openconfig/kne/topo/node/nokia"
	_ "github.com/openconfig/kne/topo/node/openconfig"
)

var (
	setPIDMaxScript       = filepath.Join(homedir.HomeDir(), "kne-internal", "set_pid_max.sh")
	protojsonUnmarshaller = protojson.UnmarshalOptions{
		AllowPartial:   true,
		DiscardUnknown: false,
	}

	// Stubs for testing.
	kindClusterIsKind = kind.ClusterIsKind
)

// Manager is a topology manager for a cluster instance.
type Manager struct {
	progress       bool
	topo           *tpb.Topology
	nodes          map[string]node.Node
	kubecfg        string
	kClient        kubernetes.Interface
	tClient        topologyclientv1.Interface
	rCfg           *rest.Config
	basePath       string
	skipDeleteWait bool

	// If reportUsage is set, report anonymous usage metrics.
	reportUsage bool
	// reportUsageProjectID is the ID of the GCP project the usage
	// metrics should be written to. This field is not used if
	// ReportUsage is unset. An empty string will result in the
	// default project being used.
	reportUsageProjectID string
	// reportUsageTopicID is the ID of the GCP PubSub topic the usage
	// metrics should be written to. This field is not used if
	// ReportUsage is unset. An empty string will result in the
	// default topic being used.
	reportUsageTopicID string
}

type Option func(m *Manager)

func WithKubecfg(k string) Option {
	return func(m *Manager) {
		m.kubecfg = k
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
		m.basePath = s
	}
}

// WithUsageReporting writes anonymous usage metrics.
func WithUsageReporting(b bool, project, topic string) Option {
	return func(m *Manager) {
		m.reportUsage = b
		m.reportUsageProjectID = project
		m.reportUsageTopicID = topic
	}
}

// WithProgress returns a Manager Option where true causes pod progress to be displayed.
func WithProgress(b bool) Option {
	return func(m *Manager) {
		m.progress = b
	}
}

// WithSkipDeleteWait will not wait for resources to be cleaned up before Delete returns.
func WithSkipDeleteWait(b bool) Option {
	return func(m *Manager) {
		m.skipDeleteWait = b
	}
}

// New creates a new Manager based on the provided topology. The cluster config
// passed from the WithClusterConfig option overrides the determined in-cluster
// config. If neither of these configurations can be used then the kubecfg passed
// from the WithKubecfg option will be used to determine the cluster config.
func New(topo *tpb.Topology, opts ...Option) (*Manager, error) {
	if topo == nil {
		return nil, fmt.Errorf("topology cannot be nil")
	}
	m := &Manager{
		topo:  topo,
		nodes: map[string]node.Node{},
	}
	for _, o := range opts {
		o(m)
	}
	if m.rCfg == nil {
		log.Infof("Trying in-cluster configuration")
		rCfg, err := rest.InClusterConfig()
		if err != nil {
			log.Infof("Falling back to kubeconfig: %q", m.kubecfg)
			rCfg, err = clientcmd.BuildConfigFromFlags("", m.kubecfg)
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
	if err := m.load(); err != nil {
		return nil, fmt.Errorf("failed to load topology: %w", err)
	}
	log.V(1).Infof("Created manager for topology:\n%v", prototext.Format(m.topo))
	return m, nil
}

// event creates a topology event protobuf from the topo.
func (m *Manager) event() *epb.Topology {
	t := &epb.Topology{
		LinkCount: int64(len(m.topo.Links)),
	}
	for _, node := range m.topo.Nodes {
		t.Nodes = append(t.Nodes, &epb.Node{
			Vendor: node.Vendor,
			Model:  node.Model,
		})
	}
	return t
}

var (
	// Stubs for testing.
	newMetricsReporter = func(ctx context.Context, project, topic string) (metricsReporter, error) {
		return metrics.NewReporter(ctx, project, topic)
	}
	deleteWatchTimeout = 30 * time.Second
)

type metricsReporter interface {
	ReportCreateTopologyStart(context.Context, *epb.Topology) (string, error)
	ReportCreateTopologyEnd(context.Context, string, error) error
	Close() error
}

func (m *Manager) reportCreateEvent(ctx context.Context) func(error) {
	r, err := newMetricsReporter(ctx, m.reportUsageProjectID, m.reportUsageTopicID)
	if err != nil {
		log.Warningf("Unable to create metrics reporter: %v", err)
		return func(_ error) {}
	}
	id, err := r.ReportCreateTopologyStart(ctx, m.event())
	if err != nil {
		log.Warningf("Unable to report create topology start event: %v", err)
		return func(_ error) { r.Close() }
	}
	return func(rerr error) {
		defer r.Close()
		if err := r.ReportCreateTopologyEnd(ctx, id, rerr); err != nil {
			log.Warningf("Unable to report create topology end event: %v", err)
		}
	}
}

// Create creates the topology in the cluster.
func (m *Manager) Create(ctx context.Context, timeout time.Duration) (rerr error) {
	log.V(1).Infof("Topology:\n%v", prototext.Format(m.topo))
	if m.reportUsage {
		finish := m.reportCreateEvent(ctx)
		defer func() { finish(rerr) }()
	}
	// Refresh cluster GAR access if needed.
	if isKind, err := kindClusterIsKind(); err == nil && isKind {
		if err := kind.RefreshGARAccess(ctx); err != nil {
			log.Warningf("Failed to refresh GAR access, cluster may need to be re-created using `kne teardown` and `kne deploy`")
		}
	}
	ctx, cancel := context.WithCancel(ctx)
	// Watch the containter status of the pods so we can fail if a container fails to start running.
	if w, err := pods.NewWatcher(ctx, m.kClient, cancel); err != nil {
		log.Warningf("Failed to start pod watcher: %v", err)
	} else {
		w.SetProgress(m.progress)
		defer func() {
			cancel()
			rerr = w.Cleanup(rerr)
		}()
	}
	if w, err := events.NewWatcher(ctx, m.kClient, cancel); err != nil {
		log.Warningf("Failed to start event watcher: %v", err)
	} else {
		w.SetProgress(m.progress)
		defer func() {
			cancel()
			rerr = w.Cleanup(rerr)
		}()
	}
	if err := m.push(ctx); err != nil {
		return fmt.Errorf("failed to create topology %q: %w", m.topo.GetName(), err)
	}
	if err := m.checkNodeStatus(ctx, timeout); err != nil {
		return fmt.Errorf("failed to check status of nodes in topology %q: %w", m.topo.GetName(), err)
	}
	log.Infof("Topology %q created", m.topo.GetName())
	return nil
}

// Delete deletes the topology from the cluster.
func (m *Manager) Delete(ctx context.Context) error {
	log.Infof("Topology:\n%v", prototext.Format(m.topo))
	if _, err := m.kClient.CoreV1().Namespaces().Get(ctx, m.topo.Name, metav1.GetOptions{}); err != nil {
		return fmt.Errorf("topology %q does not exist in cluster", m.topo.Name)
	}

	// Delete topology nodes.
	for _, n := range m.nodes {
		if err := n.Delete(ctx); err != nil {
			log.Warningf("Error deleting node %q: %v", n.Name(), err)
		}
	}

	if err := m.deleteMeshnetTopologies(ctx); err != nil {
		// Log a warning instead of failing as deleting the namespace should delete all meshnet resources.
		log.Warningf("Failed to delete meshnet topologies for topology %q: %v", m.topo.GetName(), err)
	}

	if m.skipDeleteWait {
		// Delete the namespace.
		prop := metav1.DeletePropagationForeground
		if err := m.kClient.CoreV1().Namespaces().Delete(ctx, m.topo.Name, metav1.DeleteOptions{PropagationPolicy: &prop}); err != nil {
			return fmt.Errorf("failed to delete namespace %q: %w", m.topo.Name, err)
		}
		return nil
	}

	// Watch for namespace deletion.
	c := make(chan error)
	defer close(c)
	go func() {
		tCtx, cancel := context.WithTimeout(ctx, deleteWatchTimeout)
		defer cancel()
		waitNSDeleted(tCtx, m.kClient, m.topo.Name, c)
	}()

	// Delete the namespace.
	prop := metav1.DeletePropagationForeground
	if err := m.kClient.CoreV1().Namespaces().Delete(ctx, m.topo.Name, metav1.DeleteOptions{PropagationPolicy: &prop}); err != nil {
		return fmt.Errorf("failed to delete namespace %q: %w", m.topo.Name, err)
	}

	// Wait for namespace deletion.
	log.Infof("Waiting for namespace %q to be deleted", m.topo.Name)
	if err := <-c; err != nil {
		return fmt.Errorf("failed to wait for namespace %q deletion: %w", m.topo.Name, err)
	}
	return nil
}

// waitNSDeleted waits for a namespace to be deleted. Write a non-nil error to the errCh
// if waiting is unsuccessful. Else write a nil error to errCh and then return.
func waitNSDeleted(ctx context.Context, kClient kubernetes.Interface, ns string, errCh chan error) {
	w, err := kClient.CoreV1().Namespaces().Watch(ctx, metav1.ListOptions{})
	if err != nil {
		errCh <- err
		return
	}
	for {
		select {
		case <-ctx.Done():
			errCh <- fmt.Errorf("context canceled before namespace deleted")
			return
		case e, ok := <-w.ResultChan():
			if !ok {
				errCh <- fmt.Errorf("no more watch events")
				return
			}
			n, ok := e.Object.(*corev1.Namespace)
			if !ok {
				continue
			}
			if n.Name != ns {
				continue
			}
			if e.Type == watch.Deleted {
				log.Infof("Namespace %q deleted", ns)
				errCh <- nil
				return
			}
		}
	}
}

// Show returns the topology information including services and node health.
func (m *Manager) Show(ctx context.Context) (*cpb.ShowTopologyResponse, error) {
	log.Infof("Topology:\n%v", prototext.Format(m.topo))
	r, err := m.Resources(ctx)
	if err != nil {
		return nil, err
	}
	for _, n := range m.topo.Nodes {
		pods, ok := r.Pods[n.Name]
		if !ok || len(pods) == 0 {
			return nil, fmt.Errorf("pods for node %s not found", n.Name)
		}
		n.PodIp = pods[0].Status.PodIP
		if len(n.Services) == 0 {
			continue
		}
		services, ok := r.Services[n.Name]
		if !ok {
			return nil, fmt.Errorf("services for node %s not found", n.Name)
		}
		for _, svc := range services {
			if err := populateServiceMap(svc, n.Services); err != nil {
				return nil, err
			}
		}
	}
	stateMap := &stateMap{}
	for _, n := range m.nodes {
		phase, _ := n.Status(ctx)
		stateMap.setNodeState(n.Name(), phase)
	}
	return &cpb.ShowTopologyResponse{
		State:    stateMap.topologyState(),
		Topology: m.topo,
	}, nil
}

func (m *Manager) Watch(ctx context.Context) error {
	watcher, err := m.tClient.Topology(m.topo.Name).Watch(ctx, metav1.ListOptions{})
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

// Nodes returns a map of node names to implementations in the current topology.
func (m *Manager) Nodes() map[string]node.Node {
	return m.nodes
}

// load populates the internal fields of the topology proto.
func (m *Manager) load() error {
	nMap := map[string]*tpb.Node{}
	for _, n := range m.topo.Nodes {
		if len(n.Interfaces) == 0 {
			n.Interfaces = map[string]*tpb.Interface{}
		}
		for k := range n.Interfaces {
			if n.Interfaces[k].IntName == "" {
				n.Interfaces[k].IntName = k
			}
		}
		nMap[n.Name] = n
	}
	uid := 0
	for _, l := range m.topo.Links {
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
		// Bug: Some vendors incorrectly increase the value of kernel.pid_max which
		// causes other vendors to have issues. Run this script as a temporary
		// workaround.
		if _, err := os.Stat(setPIDMaxScript); err == nil {
			if err := run.LogCommand(setPIDMaxScript); err != nil {
				return fmt.Errorf("failed to exec set_pid_max script: %w", err)
			}
		}
		log.Infof("Adding Node: %s:%s", n.Name, n.Vendor)
		nn, err := node.New(m.topo.Name, n, m.kClient, m.rCfg, m.basePath, m.kubecfg)
		if err != nil {
			return fmt.Errorf("failed to load topology: %w", err)
		}
		m.nodes[k] = nn
	}
	for _, l := range m.topo.Links {
		aNode := m.nodes[l.ANode]
		zNode := m.nodes[l.ZNode]
		loop := aNode.BackToBackLoop() && zNode.BackToBackLoop()
		if !loop && l.ANode == l.ZNode {
			return fmt.Errorf("invalid link: back to back loop %s:%s %s:%s not supported", l.ANode, l.AInt, l.ZNode, l.ZInt)
		}
	}
	return nil
}

// setLinkPeer finds the peer pod name and peer interface name for a given interface.
func setLinkPeer(nodeName string, podName string, link *topologyv1.Link, peerSpecs []*topologyv1.Topology) error {
	for _, peerSpec := range peerSpecs {
		for _, peerLink := range peerSpec.Spec.Links {
			// make sure self ifc and peer ifc belong to same link (and hence UID) but are not the same interfaces
			if peerLink.UID == link.UID && !(nodeName == link.PeerPod && peerLink.LocalIntf == link.LocalIntf) {
				link.PeerPod = peerSpec.ObjectMeta.Name
				link.PeerIntf = peerLink.LocalIntf
				return nil
			}
		}
	}
	return fmt.Errorf("could not find peer for node %s pod %s link UID %d", nodeName, podName, link.UID)
}

// topologySpecs provides a custom implementation for constructing meshnet resource specs
// (before meshnet topology creation) for all configured nodes.
func (m *Manager) topologySpecs(ctx context.Context) ([]*topologyv1.Topology, error) {
	nodeSpecs := map[string][]*topologyv1.Topology{}
	topos := []*topologyv1.Topology{}

	// get topology specs from all nodes
	for _, n := range m.nodes {
		log.Infof("Getting topology specs for node %s", n.Name())
		specs, err := n.TopologySpecs(ctx)
		if err != nil {
			return nil, fmt.Errorf("could not fetch topology specs for node %s: %v", n.Name(), err)
		}

		log.V(2).Infof("Topology specs for node %s: %+v", n.Name(), specs)
		nodeSpecs[n.Name()] = specs
	}

	// replace node name with pod name, for peer pod attribute in each link
	for nodeName, specs := range nodeSpecs {
		for _, spec := range specs {
			for l := range spec.Spec.Links {
				link := &spec.Spec.Links[l]
				peerSpecs, ok := nodeSpecs[link.PeerPod]
				if !ok {
					return nil, fmt.Errorf("specs do not exist for node %s", link.PeerPod)
				}

				if err := setLinkPeer(nodeName, spec.ObjectMeta.Name, link, peerSpecs); err != nil {
					return nil, err
				}
			}
			topos = append(topos, spec)
		}
	}

	return topos, nil
}

// push deploys the topology to the cluster.
func (m *Manager) push(ctx context.Context) error {
	log.Infof("Validating Node Constraints")
	for _, n := range m.nodes {
		if err := n.ValidateConstraints(); err != nil {
			return fmt.Errorf("failed to validate node %s: %w", n, err)
		}
	}

	if _, err := m.kClient.CoreV1().Namespaces().Get(ctx, m.topo.Name, metav1.GetOptions{}); err != nil {
		log.Infof("Creating namespace for topology: %q", m.topo.Name)
		ns := &corev1.Namespace{
			ObjectMeta: metav1.ObjectMeta{
				Name: m.topo.Name,
				Labels: map[string]string{
					"kne-topology": "true",
				},
			},
		}
		sNs, err := m.kClient.CoreV1().Namespaces().Create(ctx, ns, metav1.CreateOptions{})
		if err != nil {
			return fmt.Errorf("failed to create namespace %q: %w", ns, err)
		}
		log.Infof("Server Namespace: %+v", sNs)
	}

	if err := m.createMeshnetTopologies(ctx); err != nil {
		return fmt.Errorf("failed to create meshnet topologies: %w", err)
	}

	log.Infof("Creating Node Pods and Generating certs")

	var wg sync.WaitGroup
	errCh := make(chan error)

	for _, n := range m.nodes {
		wg.Add(1)
		go func(node node.Node) {
			defer wg.Done()

			for key, service := range node.GetProto().Services {
				updateServicePortName(service, key)
			}

			if err := node.Create(ctx); err != nil {
				errCh <- fmt.Errorf("failed to create node %s: %w", node, err)
				return
			}
			log.Infof("Node %s resource created", node)

			log.Infof("Generating Self-Signed Certificates for node %s", node)

			err := m.GenerateSelfSigned(ctx, node.Name())
			switch {
			case err == nil, status.Code(err) == codes.Unimplemented:
			default:
				errCh <- fmt.Errorf("failed to generate cert for node %s: %w", node, err)
			}
		}(n)
	}
	go func() {
		wg.Wait()
		close(errCh)
	}()

	for err := range errCh {
		return err
	}

	return nil
}

func updateServicePortName(s *tpb.Service, port uint32) {
	i := 0
	for _, name := range s.Names {
		if name != "" {
			s.Names[i] = name
			i++
		}
	}
	s.Names = s.Names[:i]

	if s.Name == "" && len(s.Names) > 0 {
		s.Name = s.Names[0]
	} else if s.Name == "" {
		s.Name = fmt.Sprintf("port-%d", port)
	}
}

// createMeshnetTopologies creates meshnet resources for all available nodes.
func (m *Manager) createMeshnetTopologies(ctx context.Context) error {
	log.Infof("Getting topology specs for namespace %s", m.topo.Name)
	topologies, err := m.topologySpecs(ctx)
	if err != nil {
		return fmt.Errorf("could not get meshnet topologies: %v", err)
	}
	log.V(2).Infof("Got topology specs for namespace %s: %+v", m.topo.Name, topologies)
	for _, t := range topologies {
		log.Infof("Creating topology for meshnet node %s", t.ObjectMeta.Name)
		sT, err := m.tClient.Topology(m.topo.Name).Create(ctx, t, metav1.CreateOptions{})
		if err != nil {
			return fmt.Errorf("could not create topology for meshnet node %s: %v", t.ObjectMeta.Name, err)
		}
		log.V(1).Infof("Meshnet Node:\n%+v\n", sT)
	}
	return nil
}

// deleteMeshnetTopologies deletes meshnet resources for all available nodes.
func (m *Manager) deleteMeshnetTopologies(ctx context.Context) error {
	nodes, err := m.topologyResources(ctx)
	if err != nil {
		return fmt.Errorf("failed to get meshnet nodes: %w", err)
	}
	var errs errlist.List
	for _, n := range nodes {
		if err := m.tClient.Topology(m.topo.Name).Delete(ctx, n.ObjectMeta.Name, metav1.DeleteOptions{}); err != nil {
			errs.Add(fmt.Errorf("failed to delete meshnet node %q: %w", n.ObjectMeta.Name, err))
		}
	}
	return errs.Err()
}

// checkNodeStatus reports node status, ignores for unimplemented nodes.
func (m *Manager) checkNodeStatus(ctx context.Context, timeout time.Duration) error {
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
			if err != nil || phase == node.StatusFailed {
				return fmt.Errorf("Node %s: Status %s Reason %v", n, phase, err)
			}
			if phase == node.StatusRunning {
				log.Infof("Node %s: Status %s", n, phase)
				processed[name] = true
			} else {
				foundAll = false
			}
		}
		time.Sleep(100 * time.Millisecond)
	}
	if !foundAll {
		log.Warningf("Failed to determine status of some node resources in %d sec", timeout)
	}
	return nil
}

type Resources struct {
	Services   map[string][]*corev1.Service
	Pods       map[string][]*corev1.Pod
	ConfigMaps map[string]*corev1.ConfigMap
	Topologies map[string]*topologyv1.Topology
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

	tList, err := m.topologyResources(ctx)
	if err != nil {
		return nil, err
	}
	for _, t := range tList {
		r.Topologies[t.Name] = t
	}

	return &r, nil
}

// topologyResources gets the topology CRDs for the cluster.
func (m *Manager) topologyResources(ctx context.Context) ([]*topologyv1.Topology, error) {
	topology, err := m.tClient.Topology(m.topo.Name).List(ctx, metav1.ListOptions{})
	if err != nil {
		return nil, fmt.Errorf("failed to get topology CRDs: %v", err)
	}

	items := make([]*topologyv1.Topology, len(topology.Items))
	for i := range items {
		items[i] = &topology.Items[i]
	}

	return items, nil
}

// ConfigPush will push config to the provided node. If the node does
// not fulfill ConfigPusher then status.Unimplemented error will be returned.
func (m *Manager) ConfigPush(ctx context.Context, nodeName string, r io.Reader) error {
	n, ok := m.nodes[nodeName]
	if !ok {
		return fmt.Errorf("node %q not found", nodeName)
	}
	cp, ok := n.(node.ConfigPusher)
	if !ok {
		return status.Errorf(codes.Unimplemented, "node %q does not implement ConfigPusher interface", nodeName)
	}
	return cp.ConfigPush(ctx, r)
}

// ResetCfg will reset the config for the provided node. If the node does
// not fulfill Resetter then status.Unimplemented error will be returned.
func (m *Manager) ResetCfg(ctx context.Context, nodeName string) error {
	n, ok := m.nodes[nodeName]
	if !ok {
		return fmt.Errorf("node %q not found", nodeName)
	}
	r, ok := n.(node.Resetter)
	if !ok {
		return status.Errorf(codes.Unimplemented, "node %q does not implement Resetter interface", nodeName)
	}
	return r.ResetCfg(ctx)
}

// GenerateSelfSigned will create self signed certs on the provided node.
// If the node does not have cert info then it is a noop. If the node does
// not fulfill Certer then status.Unimplemented error will be returned.
func (m *Manager) GenerateSelfSigned(ctx context.Context, nodeName string) error {
	n, ok := m.nodes[nodeName]
	if !ok {
		return fmt.Errorf("node %q not found", nodeName)
	}
	if n.GetProto().GetConfig().GetCert() == nil {
		log.V(1).Infof("No cert info for %q, skipping cert generation", nodeName)
		return nil
	}
	c, ok := n.(node.Certer)
	if !ok {
		return status.Errorf(codes.Unimplemented, "node %q does not implement Certer interface", nodeName)
	}
	return c.GenerateSelfSigned(ctx)
}

// populateServiceMap modifies m to contain the full service info.
var populateServiceMap = func(s *corev1.Service, m map[uint32]*tpb.Service) error {
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
				Name: p.Name,
			}
			m[k] = service
		}
		if service.Name == "" {
			service.Name = p.Name
		}
		service.Outside = k
		service.Inside = uint32(p.TargetPort.IntVal)
		service.NodePort = uint32(p.NodePort)
		service.InsideIp = s.Spec.ClusterIP
		service.OutsideIp = s.Status.LoadBalancer.Ingress[0].IP
	}
	return nil
}

// stateMap keeps the POD state of all topology nodes.
type stateMap struct {
	m map[string]node.Status
}

func (s *stateMap) size() int {
	return len(s.m)
}

func (s *stateMap) setNodeState(name string, state node.Status) {
	if s.m == nil {
		s.m = map[string]node.Status{}
	}
	s.m[name] = state
}

func (s *stateMap) topologyState() cpb.TopologyState {
	if s == nil || len(s.m) == 0 {
		return cpb.TopologyState_TOPOLOGY_STATE_UNSPECIFIED
	}
	counts := map[node.Status]int{}
	for _, state := range s.m {
		counts[state]++
	}
	switch {
	default:
		return cpb.TopologyState_TOPOLOGY_STATE_UNSPECIFIED
	case counts[node.StatusRunning] == s.size():
		return cpb.TopologyState_TOPOLOGY_STATE_RUNNING
	case counts[node.StatusFailed] > 0:
		return cpb.TopologyState_TOPOLOGY_STATE_ERROR
	case counts[node.StatusPending] > 0:
		return cpb.TopologyState_TOPOLOGY_STATE_CREATING
	}
}

// Load loads a Topology from path.
func Load(path string) (*tpb.Topology, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	t := &tpb.Topology{}
	switch {
	case strings.HasSuffix(path, ".yaml"), strings.HasSuffix(path, ".yml"):
		jsonBytes, err := yaml.YAMLToJSON(b)
		if err != nil {
			return nil, fmt.Errorf("could not parse yaml: %v", err)
		}
		if err := protojsonUnmarshaller.Unmarshal(jsonBytes, t); err != nil {
			return nil, fmt.Errorf("could not parse json: %v", err)
		}
	default:
		if err := prototext.Unmarshal(b, t); err != nil {
			return nil, err
		}
	}
	return t, nil
}
