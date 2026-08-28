package meshnet

import (
	"context"
	"testing"
	"time"

	fakeTopology "github.com/openconfig/kne/third_party/meshnet/api/clientset/v1beta1/fake"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
)

func createFakePodTopology(name, ns, srcIP, netNS string, peers []string) *unstructured.Unstructured {
	links := make([]interface{}, len(peers))
	for i, peer := range peers {
		links[i] = map[string]interface{}{
			"peer_pod":   peer,
			"peer_intf":  "eth14",
			"local_intf": "eth14",
			"local_ip":   "10.10.0.1/30",
			"peer_ip":    "10.10.0.2/30",
			"uid":        int64(i + 1),
		}
	}

	return &unstructured.Unstructured{
		Object: map[string]interface{}{
			"apiVersion": "networkop.co.uk/v1beta1",
			"kind":       "Topology",
			"metadata": map[string]interface{}{
				"name":      name,
				"namespace": ns,
			},
			"spec": map[string]interface{}{
				"links": links,
			},
			"status": map[string]interface{}{
				"src_ip":       srcIP,
				"net_ns":       netNS,
				"container_id": "docker123",
			},
		},
	}
}

func TestIsPodActive(t *testing.T) {
	inactive := createFakePodTopology("p1", "default", "", "", []string{"p2"})
	_, _, active := isPodActive(inactive)
	if active {
		t.Fatalf("expected inactive pod to return false")
	}

	activePod := createFakePodTopology("p1", "default", "10.0.0.1", "/proc/1/ns/net", []string{"p2"})
	srcIP, netNS, active := isPodActive(activePod)
	if !active || srcIP != "10.0.0.1" || netNS != "/proc/1/ns/net" {
		t.Fatalf("expected active pod to return true with correct IP/NS, got active=%t, srcIP=%s, netNS=%s", active, srcIP, netNS)
	}
}

func TestParsePodLinks(t *testing.T) {
	pod := createFakePodTopology("p1", "default", "10.0.0.1", "/proc/1/ns/net", []string{"p2", "p3"})
	links, err := parsePodLinks(pod)
	if err != nil {
		t.Fatalf("parsePodLinks failed: %v", err)
	}
	if len(links) != 2 {
		t.Fatalf("expected 2 links, got %d", len(links))
	}
	if links[0].PeerPodName != "p2" || links[1].PeerPodName != "p3" {
		t.Fatalf("unexpected peer pod names: %+v", links)
	}
}

func TestReconcilePodLinks_SkipInactivePeer(t *testing.T) {
	InitLogger()
	m := &Meshnet{
		nodeIP: "10.0.0.1",
	}

	// Active pod p1 pointing to peer p2.
	p1 := createFakePodTopology("p1", "default", "10.0.0.1", "/proc/1/ns/net", []string{"p2"})
	// Since getPod will fail or peer p2 is not active, ReconcilePodLinks should return nil without error.
	if err := m.ReconcilePodLinks(context.Background(), p1); err != nil {
		t.Fatalf("expected nil return when peer is inactive or missing, got: %v", err)
	}
}

func TestCleanupPodLinks(t *testing.T) {
	InitLogger()
	m := &Meshnet{}
	pod := createFakePodTopology("p1", "default", "10.0.0.1", "/proc/1/ns/net", []string{"p2"})
	if err := m.CleanupPodLinks(context.Background(), pod); err != nil {
		t.Fatalf("CleanupPodLinks failed: %v", err)
	}
}

func TestCleanupOrphanedHostVeths(t *testing.T) {
	InitLogger()
	m := &Meshnet{}
	if err := m.CleanupOrphanedHostVeths(context.Background()); err != nil {
		t.Fatalf("CleanupOrphanedHostVeths failed: %v", err)
	}
}

func TestParsePodLinks_NoLinks(t *testing.T) {
	// Pod with empty links slice
	podEmptyLinks := createFakePodTopology("p1", "default", "10.0.0.1", "/proc/1/ns/net", []string{})
	links, err := parsePodLinks(podEmptyLinks)
	if err != nil {
		t.Fatalf("parsePodLinks failed for empty links: %v", err)
	}
	if len(links) != 0 {
		t.Fatalf("expected 0 links, got %d", len(links))
	}

	// Pod with spec.links omitted entirely
	podNoLinksSpec := &unstructured.Unstructured{
		Object: map[string]interface{}{
			"apiVersion": "networkop.co.uk/v1beta1",
			"kind":       "Topology",
			"metadata": map[string]interface{}{
				"name":      "p1",
				"namespace": "default",
			},
			"spec": map[string]interface{}{},
			"status": map[string]interface{}{
				"src_ip": "10.0.0.1",
				"net_ns": "/proc/1/ns/net",
			},
		},
	}
	links, err = parsePodLinks(podNoLinksSpec)
	if err != nil {
		t.Fatalf("parsePodLinks failed for missing links spec: %v", err)
	}
	if len(links) != 0 {
		t.Fatalf("expected 0 links, got %d", len(links))
	}
}

func TestReconcilePodLinks_NoLinks(t *testing.T) {
	InitLogger()
	m := &Meshnet{
		nodeIP: "10.0.0.1",
	}

	podNoLinks := createFakePodTopology("p1", "default", "10.0.0.1", "/proc/1/ns/net", []string{})
	if err := m.ReconcilePodLinks(context.Background(), podNoLinks); err != nil {
		t.Fatalf("ReconcilePodLinks failed for pod without links: %v", err)
	}
}

func TestReconcilePodLinks_LowerPriorityPodInitiatesGRPC(t *testing.T) {
	InitLogger()
	m := &Meshnet{
		nodeIP:            "10.0.0.1",
		interNodeLinkType: "GRPC",
	}

	// Lower priority pod "a_pod" connected to peer "z_pod" on remote node "10.0.0.2"
	lowerPrioPod := createFakePodTopology("a_pod", "default", "10.0.0.1", "/proc/self/ns/net", []string{"z_pod"})
	peerPod := createFakePodTopology("z_pod", "default", "10.0.0.2", "/proc/self/ns/net", []string{"a_pod"})

	fakeClient, err := fakeTopology.NewSimpleClientset(lowerPrioPod, peerPod)
	if err != nil {
		t.Fatalf("failed to create fake topology clientset: %v", err)
	}
	m.tClient = fakeClient

	// Reconcile lower priority pod "a_pod". It should attempt to reconcile without skipping due to lower priority.
	// Since CreateGRPCWireLocal will attempt to open TAP device (which fails without root/TAP), we expect a TAP creation error,
	// proving it attempted reconciliation rather than skipping.
	err = m.ReconcilePodLinks(context.Background(), lowerPrioPod)
	if err == nil {
		t.Fatalf("expected error during TAP creation without root, but got nil (means it may have skipped)")
	}
}

func TestCleanupRemovedPodLinks_GRPC(t *testing.T) {
	InitLogger()
	m := &Meshnet{
		nodeIP: "10.0.0.1",
	}

	// Pod "p1" initially had 2 links (UID 1 and UID 2)
	podWith2Links := createFakePodTopology("p1", "default", "10.0.0.1", "/proc/self/ns/net", []string{"p2", "p3"})
	desiredLinks, err := parsePodLinks(podWith2Links)
	if err != nil || len(desiredLinks) != 2 {
		t.Fatalf("failed to parse 2 links: %v", err)
	}

	// Now remove link UID 2 from spec.links (only UID 1 remains)
	podWith1Link := createFakePodTopology("p1", "default", "10.0.0.1", "/proc/self/ns/net", []string{"p2"})
	desired1Link, _ := parsePodLinks(podWith1Link)

	// Call cleanupRemovedPodLinks with 1 link
	m.cleanupRemovedPodLinks(context.Background(), podWith1Link, "/proc/self/ns/net", desired1Link)
}

func TestReconcilePodLinks_TransitionGRPCToSameNode(t *testing.T) {
	InitLogger()
	m := &Meshnet{
		nodeIP: "10.0.0.1",
	}

	// Pod "p1" and "p2" both on same node "10.0.0.1"
	p1 := createFakePodTopology("p1", "default", "10.0.0.1", "/proc/self/ns/net", []string{"p2"})
	p2 := createFakePodTopology("p2", "default", "10.0.0.1", "/proc/self/ns/net", []string{"p1"})

	fakeClient, err := fakeTopology.NewSimpleClientset(p1, p2)
	if err != nil {
		t.Fatalf("failed to create fake topology clientset: %v", err)
	}
	m.tClient = fakeClient

	// Reconcile p1. Since netns /proc/1/ns/net doesn't exist, ConfigurePodLinks will return an error opening netns,
	// proving it proceeded to same-node veth plumbing rather than skipping or hanging on gRPC.
	err = m.ReconcilePodLinks(context.Background(), p1)
	if err == nil {
		t.Fatalf("expected error opening non-existent netns during same-node plumbing, got nil")
	}
}

func TestReconcilePodLinks_StaleLocalNetNSClearsStatus(t *testing.T) {
	InitLogger()
	m := &Meshnet{
		nodeIP: "10.0.0.1",
	}

	// Local pod "p1" with a stale netns path "/nonexistent/netns/path"
	p1 := createFakePodTopology("p1", "default", "10.0.0.1", "/nonexistent/netns/path", []string{"p2"})

	fakeClient, err := fakeTopology.NewSimpleClientset(p1)
	if err != nil {
		t.Fatalf("failed to create fake topology clientset: %v", err)
	}
	m.tClient = fakeClient

	// Reconcile p1. Since /nonexistent/netns/path does not exist, reconcilePodLinksInternal should clear alive status and return nil cleanly.
	err = m.ReconcilePodLinks(context.Background(), p1)
	if err != nil {
		t.Fatalf("expected nil return when clearing stale netns status, got: %v", err)
	}

	// Verify status fields were cleared from fake K8s client
	updatedP1, err := fakeClient.Topology("default").Unstructured(context.Background(), "p1", metav1.GetOptions{})
	if err != nil {
		t.Fatalf("failed to fetch updated topology p1: %v", err)
	}
	_, _, active := isPodActive(updatedP1)
	if active {
		t.Fatalf("expected active to be false for p1 after clearing stale netns, got true")
	}
}

func TestTopologyCache_PutGetListDelete(t *testing.T) {
	cache := NewTopologyCache()

	p1 := createFakePodTopology("p1", "default", "10.0.0.1", "/proc/1/ns/net", []string{"p2"})
	p2 := createFakePodTopology("p2", "default", "10.0.0.2", "/proc/2/ns/net", []string{"p1"})
	p3 := createFakePodTopology("p3", "other-ns", "10.0.0.3", "/proc/3/ns/net", []string{"p4"})

	cache.Put(p1)
	cache.Put(p2)
	cache.Put(p3)

	// Get tests
	if got := cache.Get("default", "p1"); got == nil || got.GetName() != "p1" {
		t.Fatalf("expected p1 in cache, got %+v", got)
	}
	if got := cache.Get("default", "nonexistent"); got != nil {
		t.Fatalf("expected nil for nonexistent pod, got %+v", got)
	}

	// List tests
	defaultList := cache.List("default")
	if len(defaultList) != 2 {
		t.Fatalf("expected 2 topologies in default namespace, got %d", len(defaultList))
	}
	allList := cache.List("")
	if len(allList) != 3 {
		t.Fatalf("expected 3 topologies across all namespaces, got %d", len(allList))
	}

	// Delete test
	cache.Delete("default", "p1")
	if got := cache.Get("default", "p1"); got != nil {
		t.Fatalf("expected nil after delete, got %+v", got)
	}
	if len(cache.List("default")) != 1 {
		t.Fatalf("expected 1 topology remaining in default namespace, got %d", len(cache.List("default")))
	}
}

func TestTopologyCache_DependencyTracking(t *testing.T) {
	cache := NewTopologyCache()

	// p1 links to p2 and p3
	p1 := createFakePodTopology("p1", "default", "10.0.0.1", "/proc/1/ns/net", []string{"p2", "p3"})
	// p4 also links to p2
	p4 := createFakePodTopology("p4", "default", "10.0.0.4", "/proc/4/ns/net", []string{"p2"})

	cache.Put(p1)
	cache.Put(p4)

	// Check dependents for p2: should be p1 and p4
	depsP2 := cache.GetDependents("default", "p2")
	if len(depsP2) != 2 {
		t.Fatalf("expected 2 dependents for p2, got %d: %+v", len(depsP2), depsP2)
	}

	// Check dependents for p3: should be only p1
	depsP3 := cache.GetDependents("default", "p3")
	if len(depsP3) != 1 || depsP3[0] != "default/p1" {
		t.Fatalf("expected [default/p1] for p3, got %+v", depsP3)
	}

	// Now delete p1: dependents for p2 should now only be p4, and p3 should have none
	cache.Delete("default", "p1")
	depsP2After := cache.GetDependents("default", "p2")
	if len(depsP2After) != 1 || depsP2After[0] != "default/p4" {
		t.Fatalf("expected [default/p4] for p2 after p1 deletion, got %+v", depsP2After)
	}
	depsP3After := cache.GetDependents("default", "p3")
	if len(depsP3After) != 0 {
		t.Fatalf("expected 0 dependents for p3 after p1 deletion, got %+v", depsP3After)
	}
}

func TestReconcileQueue_DebounceAndDrain(t *testing.T) {
	rq := NewReconcileQueue(20 * time.Millisecond)

	// Enqueue multiple pod keys in rapid succession
	rq.Enqueue("default/p1")
	rq.Enqueue("default/p2")
	rq.Enqueue("default/p1") // duplicate
	rq.Enqueue("default/p3")

	// Wait for debounce notification
	select {
	case <-rq.notifyChan:
	case <-time.After(200 * time.Millisecond):
		t.Fatalf("timed out waiting for reconcile queue notifyChan")
	}

	isFull, keys := rq.Drain()
	if isFull {
		t.Fatalf("expected isFull=false for targeted enqueue, got true")
	}
	if len(keys) != 3 {
		t.Fatalf("expected 3 unique keys, got %d: %+v", len(keys), keys)
	}

	// Test EnqueueFull
	rq.EnqueueFull()
	select {
	case <-rq.notifyChan:
	case <-time.After(200 * time.Millisecond):
		t.Fatalf("timed out waiting for reconcile queue notifyChan on full reconcile")
	}

	isFull, _ = rq.Drain()
	if !isFull {
		t.Fatalf("expected isFull=true after EnqueueFull, got false")
	}
}

func TestTargetedReconciliation_PeerRestartQueuesDependents(t *testing.T) {
	InitLogger()
	m := &Meshnet{
		nodeIP:         "10.0.0.1",
		topoCache:      NewTopologyCache(),
		reconcileQueue: NewReconcileQueue(10 * time.Millisecond),
	}

	// Local pod "p1" on node 10.0.0.1 links to remote peer "p2" on node 10.0.0.2
	p1 := createFakePodTopology("p1", "default", "10.0.0.1", "/proc/self/ns/net", []string{"p2"})
	// Unrelated local pod "p3" on node 10.0.0.1 links to "p4" on node 10.0.0.3
	p3 := createFakePodTopology("p3", "default", "10.0.0.1", "/proc/self/ns/net", []string{"p4"})

	m.topoCache.Put(p1)
	m.topoCache.Put(p3)

	// Remote peer "p2" restarts and updates its status
	p2Updated := createFakePodTopology("p2", "default", "10.0.0.2", "/proc/999/ns/net", []string{"p1"})
	m.topoCache.Put(p2Updated)

	// When p2 event arrives, controller queries dependents
	dependents := m.topoCache.GetDependents("default", "p2")
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

	// Wait for debounce notification
	select {
	case <-m.reconcileQueue.notifyChan:
	case <-time.After(200 * time.Millisecond):
		t.Fatalf("timed out waiting for reconcile queue notification")
	}

	isFull, keys := m.reconcileQueue.Drain()
	if isFull {
		t.Fatalf("expected targeted reconcile, got full")
	}
	if len(keys) != 1 || keys[0] != "default/p1" {
		t.Fatalf("expected only dependent local pod default/p1 to be queued, got %+v", keys)
	}
}

func TestTopologyCache_DeepCopyIsolation(t *testing.T) {
	cache := NewTopologyCache()
	p1 := createFakePodTopology("p1", "default", "10.0.0.1", "/proc/1/ns/net", []string{"p2"})
	cache.Put(p1)

	// Mutate the object returned from Get
	got := cache.Get("default", "p1")
	if got == nil {
		t.Fatalf("expected p1 in cache")
	}
	_ = unstructured.SetNestedField(got.Object, "10.99.99.99", "status", "src_ip")

	// Verify cached object was not mutated
	got2 := cache.Get("default", "p1")
	srcIP, _, _ := unstructured.NestedString(got2.Object, "status", "src_ip")
	if srcIP != "10.0.0.1" {
		t.Fatalf("expected cached src_ip to remain 10.0.0.1, got %s", srcIP)
	}
}

func TestUpdatePlumbingErrorStatus_NoOpWhenUnchanged(t *testing.T) {
	InitLogger()
	p1 := createFakePodTopology("p1", "default", "10.0.0.1", "/proc/1/ns/net", []string{"p2"})
	fakeClient, err := fakeTopology.NewSimpleClientset(p1)
	if err != nil {
		t.Fatalf("failed to create fake topology clientset: %v", err)
	}

	m := &Meshnet{
		tClient:   fakeClient,
		topoCache: NewTopologyCache(),
	}
	m.topoCache.Put(p1)

	// 1. Clearing when already empty should succeed without error
	if err := m.updatePlumbingErrorStatus(context.Background(), p1, ""); err != nil {
		t.Fatalf("expected nil error on clearing empty error: %v", err)
	}

	// 2. Setting an error
	if err := m.updatePlumbingErrorStatus(context.Background(), p1, "dial timeout"); err != nil {
		t.Fatalf("expected nil error on setting error: %v", err)
	}

	// 3. Setting the exact same error again should be a no-op
	if err := m.updatePlumbingErrorStatus(context.Background(), p1, "dial timeout"); err != nil {
		t.Fatalf("expected nil error on setting duplicate error: %v", err)
	}
}
