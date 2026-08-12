package meshnet

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/containernetworking/plugins/pkg/ns"
	"github.com/openconfig/kne/third_party/meshnet/daemon/grpcwire"
	mpb "github.com/openconfig/kne/third_party/meshnet/daemon/proto/meshnet/v1beta1"
	"github.com/openconfig/kne/third_party/meshnet/daemon/vxlan"
	"github.com/openconfig/kne/third_party/meshnet/utils/wireutil"
	"github.com/vishvananda/netlink"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/watch"
	"k8s.io/client-go/util/retry"
)

// isNetNSValid returns true if the netns path exists on the host filesystem.
func isNetNSValid(netNS string) bool {
	if netNS == "" {
		return false
	}
	_, err := os.Stat(netNS)
	return err == nil
}

// clearPodAliveStatus removes status.src_ip, status.net_ns, status.container_id, and status.plumbing_error
// from the Topology resource when a local pod container netns becomes invalid.
func (m *Meshnet) clearPodAliveStatus(ctx context.Context, topo *unstructured.Unstructured) error {
	if m.tClient == nil {
		return nil
	}
	return retry.RetryOnConflict(retry.DefaultRetry, func() error {
		latestTopo, err := m.tClient.Topology(topo.GetNamespace()).Unstructured(ctx, topo.GetName(), metav1.GetOptions{})
		if err != nil {
			return err
		}
		unstructured.RemoveNestedField(latestTopo.Object, "status", "src_ip")
		unstructured.RemoveNestedField(latestTopo.Object, "status", "net_ns")
		unstructured.RemoveNestedField(latestTopo.Object, "status", "container_id")
		unstructured.RemoveNestedField(latestTopo.Object, "status", "plumbing_error")

		_, err = m.tClient.Topology(latestTopo.GetNamespace()).Update(ctx, latestTopo, metav1.UpdateOptions{})
		if err == nil && m.topoCache != nil {
			m.topoCache.Put(latestTopo)
		}
		return err
	})
}

// toUnstructured converts any Kubernetes object into *unstructured.Unstructured.
func toUnstructured(obj interface{}) (*unstructured.Unstructured, error) {
	if u, ok := obj.(*unstructured.Unstructured); ok {
		return u, nil
	}
	data, err := runtime.DefaultUnstructuredConverter.ToUnstructured(obj)
	if err != nil {
		return nil, err
	}
	return &unstructured.Unstructured{Object: data}, nil
}

// isPodActive checks if a Topology resource has non-empty status.src_ip and status.net_ns.
func isPodActive(topo *unstructured.Unstructured) (srcIP, netNS string, active bool) {
	if topo == nil {
		return "", "", false
	}
	srcIP, _, _ = unstructured.NestedString(topo.Object, "status", "src_ip")
	netNS, _, _ = unstructured.NestedString(topo.Object, "status", "net_ns")
	active = (srcIP != "" && netNS != "")
	return srcIP, netNS, active
}

// parsePodLinks extracts all links from spec.links in a Topology resource.
func parsePodLinks(topo *unstructured.Unstructured) ([]wireutil.PodLinkConfig, error) {
	if topo == nil {
		return nil, nil
	}
	podName := topo.GetName()
	kubeNs := topo.GetNamespace()

	val, found, err := unstructured.NestedFieldNoCopy(topo.Object, "spec", "links")
	if err != nil || !found || val == nil {
		return nil, nil
	}
	remoteLinks, ok := val.([]interface{})
	if !ok {
		return nil, nil
	}

	links := make([]wireutil.PodLinkConfig, 0, len(remoteLinks))
	for _, rlItem := range remoteLinks {
		rl, ok := rlItem.(map[string]interface{})
		if !ok {
			continue
		}
		peerPod, _, _ := unstructured.NestedString(rl, "peer_pod")
		localIntf, _, _ := unstructured.NestedString(rl, "local_intf")
		localIP, _, _ := unstructured.NestedString(rl, "local_ip")
		peerIntf, _, _ := unstructured.NestedString(rl, "peer_intf")
		peerIP, _, _ := unstructured.NestedString(rl, "peer_ip")
		uid, _, _ := unstructured.NestedInt64(rl, "uid")

		links = append(links, wireutil.PodLinkConfig{
			PodName:     podName,
			PeerPodName: peerPod,
			LinkUID:     uid,
			KubeNs:      kubeNs,
			LocalIntf:   localIntf,
			LocalIP:     localIP,
			PeerIntf:    peerIntf,
			PeerIP:      peerIP,
			MTU:         1500,
		})
	}
	return links, nil
}

// ReconcilePodLinks reconciles network interface plumbing for an active pod scheduled on this node.
// It wraps reconcilePodLinksInternal and records any configuration error in the Topology resource's status.
func (m *Meshnet) ReconcilePodLinks(ctx context.Context, topo *unstructured.Unstructured) error {
	if topo == nil {
		return nil
	}
	err := m.reconcilePodLinksInternal(ctx, topo)
	if err != nil {
		if statusErr := m.updatePlumbingErrorStatus(ctx, topo, err.Error()); statusErr != nil {
			mnetdLogger.Warnf("ReconcilePodLinks: failed to update plumbing error status: %v", statusErr)
		}
		return err
	}
	if err := m.updatePlumbingErrorStatus(ctx, topo, ""); err != nil {
		mnetdLogger.Warnf("ReconcilePodLinks: failed to clear plumbing error status: %v", err)
	}
	return nil
}

// cleanupRemovedPodLinks removes any gRPC wires and netns interfaces that belong to link UIDs
// or interface names no longer present in desiredLinks for the active pod.
func (m *Meshnet) cleanupRemovedPodLinks(ctx context.Context, topo *unstructured.Unstructured, netNS string, desiredLinks []wireutil.PodLinkConfig) {
	if topo == nil || netNS == "" {
		return
	}

	desiredUIDs := make(map[int64]bool)
	desiredIntfs := make(map[string]bool)
	for _, l := range desiredLinks {
		desiredUIDs[l.LinkUID] = true
		desiredIntfs[l.LocalIntf] = true
	}

	// 1. Clean up removed gRPC wires
	existingWires, _ := grpcwire.GetWiresByPod(topo.GetNamespace(), topo.GetName())
	for _, wire := range existingWires {
		if wire == nil {
			continue
		}
		if wire.LocalPodNetNS == netNS && !desiredUIDs[int64(wire.UID)] {
			mnetdLogger.Infof("cleanupRemovedPodLinks: removing hot-deleted gRPC wire (UID %d, pod %s, intf %s)",
				wire.UID, wire.LocalPodName, wire.LocalPodIfaceName)

			if wire.PeerNodeIP != "" && wire.PeerNodeIP != m.nodeIP && wire.PeerNodeIP != "localhost" && wire.PeerNodeIP != "127.0.0.1" {
				url := fmt.Sprintf("%s:%d", wire.PeerNodeIP, wireutil.GRPCDefaultPort)
				url = strings.TrimSpace(url)
				if remoteConn, err := grpc.Dial(url, grpc.WithTransportCredentials(insecure.NewCredentials())); err == nil {
					remoteClient := mpb.NewRemoteClient(remoteConn)
					rpcCtx, rpcCancel := context.WithTimeout(ctx, 5*time.Second)
					_, _ = remoteClient.GRPCWireDownRemote(rpcCtx, &mpb.WireDef{
						TopoNs:        wire.TopoNamespace,
						LocalPodName:  wire.LocalPodName,
						LocalPodNetNs: wire.LocalPodNetNS,
						LinkUid:       int64(wire.UID),
					})
					rpcCancel()
					remoteConn.Close()
				}
			}

			_ = grpcwire.RemoveWireAcrosAll(wire, true)
		}
	}

	// 2. Clean up removed interfaces inside container netns
	if podNs, err := ns.GetNS(netNS); err == nil {
		_ = podNs.Do(func(_ ns.NetNS) error {
			if list, err := netlink.LinkList(); err == nil {
				for _, l := range list {
					name := l.Attrs().Name
					if name == "lo" || name == "eth0" {
						continue
					}
					if !desiredIntfs[name] {
						mnetdLogger.Infof("cleanupRemovedPodLinks: removing hot-deleted interface %s from netns %s (pod %s)", name, netNS, topo.GetName())
						_ = netlink.LinkDel(l)
					}
				}
			}
			return nil
		})
		podNs.Close()
	}
}

// reconcilePodLinksInternal performs the actual network interface plumbing work.
func (m *Meshnet) reconcilePodLinksInternal(ctx context.Context, topo *unstructured.Unstructured) error {
	if topo == nil {
		return nil
	}
	srcIP, _, _ := unstructured.NestedString(topo.Object, "status", "src_ip")
	currentNetNS, _, _ := unstructured.NestedString(topo.Object, "status", "net_ns")
	if srcIP != "" && m.nodeIP != "" && srcIP == m.nodeIP {
		if currentNetNS != "" && !isNetNSValid(currentNetNS) {
			mnetdLogger.Warnf("ReconcilePodLinks: local pod %s has stale netns path %s; clearing active status", topo.GetName(), currentNetNS)
			_ = m.CleanupPodLinks(ctx, topo)
			_ = grpcwire.DeletePodWires(topo.GetNamespace(), topo.GetName())
			_ = m.clearPodAliveStatus(ctx, topo)
			return nil
		}
	}

	srcIP, netNS, active := isPodActive(topo)
	if !active {
		return nil
	}
	if m.nodeIP != "" && srcIP != m.nodeIP {
		return nil
	}

	links, err := parsePodLinks(topo)
	if err != nil {
		return err
	}

	m.cleanupRemovedPodLinks(ctx, topo, netNS, links)

	if len(links) == 0 {
		return nil
	}

	peerCache := make(map[string]*unstructured.Unstructured)
	type grpcPeerBatch struct {
		peerIP   string
		links    []wireutil.PodLinkConfig
		wireDefs []*mpb.WireDef
	}
	grpcBatches := make(map[string]*grpcPeerBatch)

	sameNodeLinks := make([]wireutil.PodLinkConfig, 0, len(links))
	for _, link := range links {
		peerTopo, ok := peerCache[link.PeerPodName]
		if !ok {
			var err error
			peerTopo, err = m.getPod(ctx, link.PeerPodName, link.KubeNs)
			if err != nil {
				peerCache[link.PeerPodName] = nil
				continue
			}
			peerCache[link.PeerPodName] = peerTopo
		}
		if peerTopo == nil {
			continue
		}
		peerSrcIP, peerNetNS, peerActive := isPodActive(peerTopo)
		if !peerActive {
			continue
		}
		if peerSrcIP == srcIP || (m.nodeIP == "" && srcIP == "") {
			if peerNetNS != "" {
				// Transition check: If moving from gRPC to same-node veth, clean up any existing gRPC wire
				if wire, ok := grpcwire.GetWireByUID(netNS, int(link.LinkUID)); ok && wire != nil {
					mnetdLogger.Infof("ReconcilePodLinks: link UID %d for pod %s moved to same node; tearing down old gRPC wire", link.LinkUID, topo.GetName())
					_ = grpcwire.RemoveWireAcrosAll(wire, true)
				}

				// If an interface with link.LocalIntf exists in netNS but is not a veth link (e.g. old TAP or VXLAN), remove it
				if podNs, err := ns.GetNS(netNS); err == nil {
					_ = podNs.Do(func(_ ns.NetNS) error {
						if l, err := netlink.LinkByName(link.LocalIntf); err == nil {
							if l.Type() != "veth" {
								mnetdLogger.Infof("ReconcilePodLinks: removing non-veth interface %s (%s) from netns %s before same-node veth plumbing", link.LocalIntf, l.Type(), netNS)
								_ = netlink.LinkDel(l)
							}
						}
						return nil
					})
					podNs.Close()
				}

				sameNodeLinks = append(sameNodeLinks, link)
			}
		} else if peerSrcIP != "" {
			if m.interNodeLinkType == wireutil.INTER_NODE_LINK_GRPC {
				// Transition check: If moving from same-node veth or VXLAN to gRPC, clean up non-TAP interface in netNS
				if podNs, err := ns.GetNS(netNS); err == nil {
					_ = podNs.Do(func(_ ns.NetNS) error {
						if l, err := netlink.LinkByName(link.LocalIntf); err == nil {
							if l.Type() != "tuntap" {
								mnetdLogger.Infof("ReconcilePodLinks: removing non-TAP interface %s (%s) from netns %s before gRPC wire plumbing", link.LocalIntf, l.Type(), netNS)
								_ = netlink.LinkDel(l)
							}
						}
						return nil
					})
					podNs.Close()
				}

				// Check if wire already exists, is ready, and points to the current peer node IP
				if wire, ok := grpcwire.GetWireByUID(netNS, int(link.LinkUID)); ok && wire != nil && wire.IsReady && wire.PeerNodeIP == peerSrcIP {
					mnetdLogger.Debugf("ReconcilePodLinks: gRPC wire already exists for link UID %d to peer %s (%s), skipping", link.LinkUID, link.PeerPodName, peerSrcIP)
					continue
				}

				mnetdLogger.Infof("ReconcilePodLinks: initiating gRPC wire for pod %s <-> peer %s (%s, UID %d)",
					topo.GetName(), link.PeerPodName, peerSrcIP, link.LinkUID)

				// 1. Register local end in meshnet daemon (creates/attaches TAP interface in container netns)
				wireDefLocal := &mpb.WireDef{
					LocalPodNetNs: netNS,
					LinkUid:       link.LinkUID,
					TopoNs:        link.KubeNs,
					LocalPodName:  topo.GetName(),
					IntfNameInPod: link.LocalIntf,
					LocalPodIp:    link.LocalIP,
					PeerNodeIp:    peerSrcIP,
				}
				if _, err := grpcwire.CreateGRPCWireLocal(ctx, wireDefLocal); err != nil {
					mnetdLogger.Errorf("ReconcilePodLinks: failed to register local GRPC wire: %v", err)
					return err
				}

				locWire, ok := grpcwire.GetWireByUID(netNS, int(link.LinkUID))
				if !ok || locWire == nil {
					mnetdLogger.Errorf("ReconcilePodLinks: failed to get local wire for link UID %d", link.LinkUID)
					return fmt.Errorf("local wire not found for link UID %d", link.LinkUID)
				}

				pBatch := grpcBatches[peerSrcIP]
				if pBatch == nil {
					pBatch = &grpcPeerBatch{peerIP: peerSrcIP}
					grpcBatches[peerSrcIP] = pBatch
				}
				pBatch.links = append(pBatch.links, link)
				pBatch.wireDefs = append(pBatch.wireDefs, &mpb.WireDef{
					WireIfIdOnPeerNode: locWire.LocalNodeIfaceID,
					PeerNodeIp:         srcIP,
					IntfNameInPod:      link.PeerIntf,
					LocalPodNetNs:      peerNetNS,
					LocalPodName:       link.PeerPodName,
					LinkUid:            link.LinkUID,
					TopoNs:             link.KubeNs,
					LocalPodIp:         link.PeerIP,
				})
			} else {
				remotePod := &mpb.RemotePod{
					NetNs:    netNS,
					IntfName: link.LocalIntf,
					IntfIp:   link.LocalIP,
					PeerVtep: peerSrcIP,
					Vni:      link.LinkUID + wireutil.NamespaceVNIOffset(link.KubeNs),
					KubeNs:   link.KubeNs,
				}
				mnetdLogger.Infof("ReconcilePodLinks: configuring remote VXLAN link for pod %s interface %s (peer %s on VTEP %s, VNI %d)",
					topo.GetName(), link.LocalIntf, link.PeerPodName, peerSrcIP, remotePod.Vni)
				if err := vxlan.CreateOrUpdate(remotePod); err != nil {
					mnetdLogger.Errorf("ReconcilePodLinks: failed to create VXLAN for pod %s: %v", topo.GetName(), err)
					return err
				}
			}
		}
	}

	// 2. Process gRPC peer batches in chunks (default 50 items per RPC) to allow pipelined processing
	batchSize := wireutil.GetEnvInt("WIRE_BATCH_SIZE", 50)
	var batchErrs []error

	for peerIP, pBatch := range grpcBatches {
		url := fmt.Sprintf("%s:%d", peerIP, wireutil.GRPCDefaultPort)
		url = strings.TrimSpace(url)
		remoteConn, err := grpc.Dial(url, grpc.WithTransportCredentials(insecure.NewCredentials()))
		if err != nil {
			mnetdLogger.Errorf("ReconcilePodLinks: failed to dial remote node %s: %v", url, err)
			return err
		}

		remoteClient := mpb.NewRemoteClient(remoteConn)
		total := len(pBatch.wireDefs)

		for i := 0; i < total; i += batchSize {
			end := i + batchSize
			if end > total {
				end = total
			}

			chunkWireDefs := pBatch.wireDefs[i:end]
			chunkLinks := pBatch.links[i:end]

			mnetdLogger.Infof("ReconcilePodLinks: calling AddGRPCWiresRemoteBatch on %s for batch [%d:%d] of %d links", url, i, end, total)
			rpcCtx, rpcCancel := context.WithTimeout(ctx, 5*time.Second)
			batchResp, err := remoteClient.AddGRPCWiresRemoteBatch(rpcCtx, &mpb.WireDefBatch{Items: chunkWireDefs})
			rpcCancel()
			if err != nil {
				remoteConn.Close()
				mnetdLogger.Errorf("ReconcilePodLinks: AddGRPCWiresRemoteBatch failed to %s for batch [%d:%d]: %v", url, i, end, err)
				return fmt.Errorf("AddGRPCWiresRemoteBatch failed: %v", err)
			}

			if batchResp == nil {
				remoteConn.Close()
				mnetdLogger.Errorf("ReconcilePodLinks: nil batch response from %s for batch [%d:%d]", url, i, end)
				return fmt.Errorf("nil batch response from %s", url)
			}

			for j, res := range batchResp.Items {
				if j >= len(chunkLinks) {
					mnetdLogger.Warnf("ReconcilePodLinks: response items length (%d) exceeds chunk links length (%d)", len(batchResp.Items), len(chunkLinks))
					break
				}
				l := chunkLinks[j]
				if res != nil && res.Response {
					grpcwire.UpdateWireByUID(netNS, int(l.LinkUID), res.PeerIntfId, peerIP, make(chan struct{}))
				} else {
					linkErr := fmt.Errorf("remote wire creation failed for link UID %d (%s@%s -> %s@%s)", l.LinkUID, topo.GetName(), l.LocalIntf, l.PeerPodName, l.PeerIntf)
					mnetdLogger.Errorf("ReconcilePodLinks: %v", linkErr)
					batchErrs = append(batchErrs, linkErr)
				}
			}
		}

		remoteConn.Close()
	}

	if len(batchErrs) > 0 {
		return errors.Join(batchErrs...)
	}

	if len(sameNodeLinks) > 0 {
		mnetdLogger.Infof("ReconcilePodLinks: configuring %d active same-node links for pod %s (%s)", len(sameNodeLinks), topo.GetName(), netNS)
		if err := wireutil.ConfigurePodLinks(netNS, sameNodeLinks); err != nil {
			mnetdLogger.Errorf("ReconcilePodLinks: error configuring pod %s: %v", topo.GetName(), err)
			return err
		}
	}
	return nil
}

// CleanupPodLinks cleans up lingering host veth ends when a pod is deleted or deactivated.
func (m *Meshnet) CleanupPodLinks(ctx context.Context, topo *unstructured.Unstructured) error {
	if topo == nil {
		return nil
	}
	links, err := parsePodLinks(topo)
	if err != nil {
		return err
	}
	for _, link := range links {
		localHostName, _ := wireutil.HostVethNames(link.KubeNs, link.PodName, link.PeerPodName, link.LinkUID)
		if l, err := netlink.LinkByName(localHostName); err == nil {
			mnetdLogger.Infof("CleanupPodLinks: removing lingering host veth %s for deleted pod %s", localHostName, topo.GetName())
			_ = netlink.LinkDel(l)
		}
	}
	return nil
}

// TopologyCache provides a thread-safe in-memory cache of Kubernetes Topology CRs
// and maintains an inverted dependency map (peerPod -> []localPods) for targeted reconciliation.
type TopologyCache struct {
	mu       sync.RWMutex
	topos    map[string]*unstructured.Unstructured // key: "namespace/name"
	peerDeps map[string]map[string]bool            // key: "namespace/peerPodName" -> set of "namespace/dependentPodName"
}

// NewTopologyCache creates a new empty TopologyCache instance.
func NewTopologyCache() *TopologyCache {
	return &TopologyCache{
		topos:    make(map[string]*unstructured.Unstructured),
		peerDeps: make(map[string]map[string]bool),
	}
}

// Put updates or inserts a Topology resource into the cache and indexes its link dependencies.
func (c *TopologyCache) Put(topo *unstructured.Unstructured) {
	if c == nil || topo == nil {
		return
	}
	c.mu.Lock()
	defer c.mu.Unlock()

	ns := topo.GetNamespace()
	name := topo.GetName()
	key := fmt.Sprintf("%s/%s", ns, name)

	// Remove old peer dependencies registered by this pod if already in cache
	if oldTopo, exists := c.topos[key]; exists {
		oldLinks, _ := parsePodLinks(oldTopo)
		for _, l := range oldLinks {
			oldPeerKey := fmt.Sprintf("%s/%s", l.KubeNs, l.PeerPodName)
			if deps, ok := c.peerDeps[oldPeerKey]; ok {
				delete(deps, key)
				if len(deps) == 0 {
					delete(c.peerDeps, oldPeerKey)
				}
			}
		}
	}

	c.topos[key] = topo

	// Index new peer dependencies
	links, _ := parsePodLinks(topo)
	for _, l := range links {
		peerKey := fmt.Sprintf("%s/%s", l.KubeNs, l.PeerPodName)
		if c.peerDeps[peerKey] == nil {
			c.peerDeps[peerKey] = make(map[string]bool)
		}
		c.peerDeps[peerKey][key] = true
	}
}

// Delete removes a Topology resource from the cache and cleans up its link dependencies.
func (c *TopologyCache) Delete(ns, name string) {
	if c == nil {
		return
	}
	c.mu.Lock()
	defer c.mu.Unlock()

	key := fmt.Sprintf("%s/%s", ns, name)
	if oldTopo, exists := c.topos[key]; exists {
		oldLinks, _ := parsePodLinks(oldTopo)
		for _, l := range oldLinks {
			oldPeerKey := fmt.Sprintf("%s/%s", l.KubeNs, l.PeerPodName)
			if deps, ok := c.peerDeps[oldPeerKey]; ok {
				delete(deps, key)
				if len(deps) == 0 {
					delete(c.peerDeps, oldPeerKey)
				}
			}
		}
		delete(c.topos, key)
	}
}

// Get retrieves a Topology resource by namespace and name from the cache.
func (c *TopologyCache) Get(ns, name string) *unstructured.Unstructured {
	if c == nil {
		return nil
	}
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.topos[fmt.Sprintf("%s/%s", ns, name)]
}

// List returns all cached Topology resources matching the given namespace (or all if namespace is empty or metav1.NamespaceAll).
func (c *TopologyCache) List(ns string) []*unstructured.Unstructured {
	if c == nil {
		return nil
	}
	c.mu.RLock()
	defer c.mu.RUnlock()

	res := make([]*unstructured.Unstructured, 0, len(c.topos))
	for _, topo := range c.topos {
		if ns == "" || ns == metav1.NamespaceAll || topo.GetNamespace() == ns {
			res = append(res, topo)
		}
	}
	return res
}

// GetDependents returns all pod keys ("namespace/name") that declare links pointing to the given pod.
func (c *TopologyCache) GetDependents(ns, name string) []string {
	if c == nil {
		return nil
	}
	c.mu.RLock()
	defer c.mu.RUnlock()

	key := fmt.Sprintf("%s/%s", ns, name)
	deps := c.peerDeps[key]
	if len(deps) == 0 {
		return nil
	}
	result := make([]string, 0, len(deps))
	for depKey := range deps {
		result = append(result, depKey)
	}
	return result
}

// ReconcileQueue coalesces and debounces reconciliation requests for individual pods
// or full cluster sync passes.
type ReconcileQueue struct {
	mu          sync.Mutex
	pending     map[string]bool
	fullPending bool
	notifyChan  chan struct{}
	debounceDur time.Duration
	timer       *time.Timer
}

// NewReconcileQueue creates a new ReconcileQueue with the specified debounce duration.
func NewReconcileQueue(debounceDur time.Duration) *ReconcileQueue {
	if debounceDur <= 0 {
		debounceDur = 50 * time.Millisecond
	}
	return &ReconcileQueue{
		pending:     make(map[string]bool),
		notifyChan:  make(chan struct{}, 1),
		debounceDur: debounceDur,
	}
}

// Enqueue adds a pod key ("namespace/name") to the pending reconciliation set and arms the debounce timer.
func (rq *ReconcileQueue) Enqueue(key string) {
	if rq == nil {
		return
	}
	rq.mu.Lock()
	defer rq.mu.Unlock()

	rq.pending[key] = true
	if rq.timer == nil {
		rq.timer = time.AfterFunc(rq.debounceDur, func() {
			select {
			case rq.notifyChan <- struct{}{}:
			default:
			}
		})
	}
}

// EnqueueFull requests a full cluster reconciliation pass and arms the debounce timer.
func (rq *ReconcileQueue) EnqueueFull() {
	if rq == nil {
		return
	}
	rq.mu.Lock()
	defer rq.mu.Unlock()

	rq.fullPending = true
	if rq.timer == nil {
		rq.timer = time.AfterFunc(rq.debounceDur, func() {
			select {
			case rq.notifyChan <- struct{}{}:
			default:
			}
		})
	}
}

// Drain clears and returns the currently queued reconciliation tasks.
func (rq *ReconcileQueue) Drain() (bool, []string) {
	if rq == nil {
		return true, nil
	}
	rq.mu.Lock()
	defer rq.mu.Unlock()

	isFull := rq.fullPending
	rq.fullPending = false

	keys := make([]string, 0, len(rq.pending))
	for k := range rq.pending {
		keys = append(keys, k)
	}
	rq.pending = make(map[string]bool)
	rq.timer = nil

	return isFull, keys
}

func parseKey(key string) (string, string) {
	parts := strings.SplitN(key, "/", 2)
	if len(parts) == 2 {
		return parts[0], parts[1]
	}
	return "", parts[0]
}

func (m *Meshnet) enqueueReconcile(key string) {
	if m.reconcileQueue != nil {
		m.reconcileQueue.Enqueue(key)
	} else {
		m.triggerReconcile()
	}
}

func (m *Meshnet) enqueueFullReconcile() {
	if m.reconcileQueue != nil {
		m.reconcileQueue.EnqueueFull()
	} else {
		m.triggerReconcile()
	}
}

// CleanupOrphanedHostVeths scans the host network namespace for any temporary host veths ("vnm-...")
// that do not match any link in any currently existing Topology resource and deletes them.
// This cleans up partial veths left behind by topologies that were deleted while meshnetd was offline.
func (m *Meshnet) CleanupOrphanedHostVeths(ctx context.Context) error {
	var topos []*unstructured.Unstructured
	if m.topoCache != nil {
		topos = m.topoCache.List(metav1.NamespaceAll)
	}
	if len(topos) == 0 && m.tClient != nil {
		list, err := m.tClient.Topology(metav1.NamespaceAll).List(ctx, metav1.ListOptions{})
		if err != nil {
			return err
		}
		for i := range list.Items {
			u, err := toUnstructured(&list.Items[i])
			if err != nil {
				continue
			}
			topos = append(topos, u)
		}
	}

	validHostNames := make(map[string]bool)
	for _, u := range topos {
		links, _ := parsePodLinks(u)
		for _, l := range links {
			side0, side1 := wireutil.HostVethNames(l.KubeNs, l.PodName, l.PeerPodName, l.LinkUID)
			validHostNames[side0] = true
			validHostNames[side1] = true
		}
	}

	hostLinks, err := netlink.LinkList()
	if err != nil {
		return err
	}

	for _, l := range hostLinks {
		name := l.Attrs().Name
		if strings.HasPrefix(name, "vnm-") && !validHostNames[name] {
			mnetdLogger.Infof("CleanupOrphanedHostVeths: removing orphaned host veth %s (no matching Topology link)", name)
			_ = netlink.LinkDel(l)
		}
	}
	return nil
}

// ReconcileAllLocalPods scans all Topology resources and reconciles any active local pod scheduled on this node.
func (m *Meshnet) ReconcileAllLocalPods(ctx context.Context) error {
	var topos []*unstructured.Unstructured
	if m.topoCache != nil {
		topos = m.topoCache.List(metav1.NamespaceAll)
	}
	if len(topos) == 0 && m.tClient != nil {
		list, err := m.tClient.Topology(metav1.NamespaceAll).List(ctx, metav1.ListOptions{})
		if err != nil {
			return err
		}
		for i := range list.Items {
			u, err := toUnstructured(&list.Items[i])
			if err != nil {
				continue
			}
			topos = append(topos, u)
		}
	}
	for _, u := range topos {
		_ = m.ReconcilePodLinks(ctx, u)
	}
	return nil
}

// triggerReconcile triggers a full reconciliation pass via the reconcile queue.
func (m *Meshnet) triggerReconcile() {
	m.enqueueFullReconcile()
}

// runReconcileWorker runs in the background and coalesces incoming reconcile triggers.
// When a trigger is received, it executes a targeted or full local reconciliation pass.
func (m *Meshnet) runReconcileWorker(ctx context.Context) {
	if m.reconcileQueue == nil {
		m.reconcileQueue = NewReconcileQueue(50 * time.Millisecond)
	}

	for {
		select {
		case <-ctx.Done():
			return
		case <-m.reconcileQueue.notifyChan:
			isFull, keys := m.reconcileQueue.Drain()
			if isFull {
				_ = m.CleanupOrphanedHostVeths(ctx)
				_ = m.ReconcileAllLocalPods(ctx)
			} else {
				for _, key := range keys {
					ns, name := parseKey(key)
					topo, err := m.getPod(ctx, name, ns)
					if err != nil || topo == nil {
						continue
					}
					_ = m.ReconcilePodLinks(ctx, topo)
				}
			}
		}
	}
}

// RunControllerLoop runs the continuous level-triggered Topology controller in meshnetd.
// It maintains an in-memory topology cache, tracks pod-link dependencies, and coalesces
// incoming events into targeted background reconciliation runs.
func (m *Meshnet) RunControllerLoop(ctx context.Context) {
	mnetdLogger.Infof("Starting Topology controller loop")
	if m.topoCache == nil {
		m.topoCache = NewTopologyCache()
	}
	if m.reconcileQueue == nil {
		m.reconcileQueue = NewReconcileQueue(50 * time.Millisecond)
	}

	// 1. Initial full population of cache from K8s API
	if m.tClient != nil {
		list, err := m.tClient.Topology(metav1.NamespaceAll).List(ctx, metav1.ListOptions{})
		if err == nil && list != nil {
			for i := range list.Items {
				if u, err := toUnstructured(&list.Items[i]); err == nil && u != nil {
					m.topoCache.Put(u)
				}
			}
		} else if err != nil {
			mnetdLogger.Warnf("RunControllerLoop: initial list failed: %v", err)
		}
	}

	go m.runReconcileWorker(ctx)

	// Trigger initial full reconciliation on startup
	m.enqueueFullReconcile()

	// Periodic resync ticker (every 60s) as a safety net against missed watch events or state decay
	resyncTicker := time.NewTicker(60 * time.Second)
	defer resyncTicker.Stop()

	for {
		if ctx.Err() != nil {
			return
		}
		watcher, err := m.tClient.Topology(metav1.NamespaceAll).Watch(ctx, metav1.ListOptions{})
		if err != nil {
			mnetdLogger.Errorf("RunControllerLoop: watch error: %v, retrying in 2s", err)
			time.Sleep(2 * time.Second)
			continue
		}

		watchCh := watcher.ResultChan()
		loopDone := false
		for !loopDone {
			select {
			case <-ctx.Done():
				watcher.Stop()
				return
			case <-resyncTicker.C:
				m.enqueueFullReconcile()
			case event, ok := <-watchCh:
				if !ok {
					loopDone = true
					break
				}
				topo, err := toUnstructured(event.Object)
				if err != nil || topo == nil {
					continue
				}

				ns := topo.GetNamespace()
				name := topo.GetName()
				key := fmt.Sprintf("%s/%s", ns, name)

				switch event.Type {
				case watch.Added, watch.Modified:
					// Update cache
					m.topoCache.Put(topo)

					// Check if this pod is local to this node
					srcIP, _, active := isPodActive(topo)
					isLocal := active && (m.nodeIP == "" || srcIP == m.nodeIP)
					if isLocal {
						m.enqueueReconcile(key)
					}

					// Find all local dependent pods that have links to this pod
					dependents := m.topoCache.GetDependents(ns, name)
					for _, depKey := range dependents {
						depNS, depName := parseKey(depKey)
						depTopo := m.topoCache.Get(depNS, depName)
						if depTopo != nil {
							depSrcIP, _, depActive := isPodActive(depTopo)
							if depActive && (m.nodeIP == "" || depSrcIP == m.nodeIP) {
								m.enqueueReconcile(depKey)
							}
						}
					}

				case watch.Deleted:
					// Before removing from cache, get all dependent pods
					dependents := m.topoCache.GetDependents(ns, name)
					m.topoCache.Delete(ns, name)

					// If this was a local pod, clean up its links
					_ = m.CleanupPodLinks(ctx, topo)

					// Reconcile dependent pods so they update / clean up their link state
					for _, depKey := range dependents {
						depNS, depName := parseKey(depKey)
						depTopo := m.topoCache.Get(depNS, depName)
						if depTopo != nil {
							depSrcIP, _, depActive := isPodActive(depTopo)
							if depActive && (m.nodeIP == "" || depSrcIP == m.nodeIP) {
								m.enqueueReconcile(depKey)
							}
						}
					}
				}
			}
		}
		// If watch closed, queue full resync on reconnect
		m.enqueueFullReconcile()
	}
}

// updatePlumbingErrorStatus writes the provided plumbing error message (or clears it if empty)
// to the Topology resource's status.plumbing_error field.
func (m *Meshnet) updatePlumbingErrorStatus(ctx context.Context, topo *unstructured.Unstructured, errMsg string) error {
	if m.tClient == nil {
		return fmt.Errorf("topology client not initialized")
	}

	err := retry.RetryOnConflict(retry.DefaultRetry, func() error {
		latestTopo, err := m.tClient.Topology(topo.GetNamespace()).Unstructured(ctx, topo.GetName(), metav1.GetOptions{})
		if err != nil {
			return err
		}

		if errMsg == "" {
			unstructured.RemoveNestedField(latestTopo.Object, "status", "plumbing_error")
		} else {
			if err := unstructured.SetNestedField(latestTopo.Object, errMsg, "status", "plumbing_error"); err != nil {
				return err
			}
		}

		_, err = m.tClient.Topology(latestTopo.GetNamespace()).Update(ctx, latestTopo, metav1.UpdateOptions{})
		if err == nil && m.topoCache != nil {
			m.topoCache.Put(latestTopo)
		}
		return err
	})

	return err
}
