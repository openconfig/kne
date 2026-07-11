package meshnet

import (
	"context"
	"testing"

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
