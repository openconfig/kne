package meshnet

import (
	"context"
	"strings"
	"time"

	"github.com/openconfig/kne/third_party/meshnet/utils/wireutil"
	"github.com/vishvananda/netlink"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/watch"
)

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

	remoteLinks, found, err := unstructured.NestedSlice(topo.Object, "spec", "links")
	if err != nil || !found || remoteLinks == nil {
		return nil, err
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
		uid, _, _ := unstructured.NestedInt64(rl, "uid")

		links = append(links, wireutil.PodLinkConfig{
			PodName:     podName,
			PeerPodName: peerPod,
			LinkUID:     uid,
			KubeNs:      kubeNs,
			LocalIntf:   localIntf,
			LocalIP:     localIP,
			MTU:         1500,
		})
	}
	return links, nil
}

// ReconcilePodLinks reconciles network interface plumbing for an active pod scheduled on this node.
// For all active same-node peer pods, it invokes wireutil.ConfigurePodLinks.
func (m *Meshnet) ReconcilePodLinks(ctx context.Context, topo *unstructured.Unstructured) error {
	if topo == nil {
		return nil
	}
	srcIP, netNS, active := isPodActive(topo)
	if !active {
		return nil
	}
	if m.nodeIP != "" && srcIP != m.nodeIP {
		return nil
	}

	links, err := parsePodLinks(topo)
	if err != nil || len(links) == 0 {
		return err
	}

	peerCache := make(map[string]*unstructured.Unstructured)
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
				sameNodeLinks = append(sameNodeLinks, link)
			}
		}
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

// CleanupOrphanedHostVeths scans the host network namespace for any temporary host veths ("vnm-...")
// that do not match any link in any currently existing Topology resource and deletes them.
// This cleans up partial veths left behind by topologies that were deleted while meshnetd was offline.
func (m *Meshnet) CleanupOrphanedHostVeths(ctx context.Context) error {
	if m.tClient == nil {
		return nil
	}
	list, err := m.tClient.Topology(metav1.NamespaceAll).List(ctx, metav1.ListOptions{})
	if err != nil {
		return err
	}

	validHostNames := make(map[string]bool)
	for i := range list.Items {
		u, err := toUnstructured(&list.Items[i])
		if err != nil {
			continue
		}
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
	if m.tClient == nil {
		return nil
	}
	list, err := m.tClient.Topology(metav1.NamespaceAll).List(ctx, metav1.ListOptions{})
	if err != nil {
		return err
	}
	for i := range list.Items {
		u, err := toUnstructured(&list.Items[i])
		if err != nil {
			continue
		}
		_ = m.ReconcilePodLinks(ctx, u)
	}
	return nil
}

// triggerReconcile sets the dirty token in dirtyChan, coalescing multiple triggers
// into at most one pending reconciliation run.
func (m *Meshnet) triggerReconcile() {
	if m.dirtyChan == nil {
		return
	}
	select {
	case m.dirtyChan <- struct{}{}:
	default:
		// Already triggered/dirty; worker will execute a pass covering all updates.
	}
}

// runReconcileWorker runs in the background and coalesces incoming reconcile triggers.
// When a trigger is received, it executes a full local reconciliation pass. Any triggers that
// arrive while reconciliation is in progress coalesce into a single follow-up pass.
func (m *Meshnet) runReconcileWorker(ctx context.Context) {
	for {
		select {
		case <-ctx.Done():
			return
		case <-m.dirtyChan:
			_ = m.CleanupOrphanedHostVeths(ctx)
			_ = m.ReconcileAllLocalPods(ctx)
		}
	}
}

// RunControllerLoop runs the continuous level-triggered Topology controller in meshnetd.
// It watches for resource changes across namespaces and coalesces incoming events into
// background reconciliation runs.
func (m *Meshnet) RunControllerLoop(ctx context.Context) {
	mnetdLogger.Infof("Starting Topology controller loop")
	go m.runReconcileWorker(ctx)
	m.triggerReconcile()

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

		for event := range watcher.ResultChan() {
			if ctx.Err() != nil {
				watcher.Stop()
				return
			}
			topo, err := toUnstructured(event.Object)
			if err != nil || topo == nil {
				continue
			}
			switch event.Type {
			case watch.Added, watch.Modified:
				m.triggerReconcile()
			case watch.Deleted:
				_ = m.CleanupPodLinks(ctx, topo)
				m.triggerReconcile()
			}
		}
		m.triggerReconcile()
	}
}
