package meshnet

import (
	"context"
	"testing"

	fakeTopology "github.com/openconfig/kne/third_party/meshnet/api/clientset/v1beta1/fake"
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
	lowerPrioPod := createFakePodTopology("a_pod", "default", "10.0.0.1", "/proc/100/ns/net", []string{"z_pod"})
	peerPod := createFakePodTopology("z_pod", "default", "10.0.0.2", "/proc/200/ns/net", []string{"a_pod"})

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

