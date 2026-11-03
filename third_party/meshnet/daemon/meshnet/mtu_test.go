package meshnet

import (
	"context"
	"testing"

	mpb "github.com/openconfig/kne/third_party/meshnet/daemon/proto/meshnet/v1beta1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
)

// withLinkMTU sets the mtu of the link with the given UID in topo.
func withLinkMTU(t *testing.T, topo *unstructured.Unstructured, uid int64, mtu int64) *unstructured.Unstructured {
	t.Helper()
	links, _, _ := unstructured.NestedSlice(topo.Object, "spec", "links")
	for _, l := range links {
		lm := l.(map[string]interface{})
		if lm["uid"] == uid {
			lm["mtu"] = mtu
		}
	}
	if err := unstructured.SetNestedSlice(topo.Object, links, "spec", "links"); err != nil {
		t.Fatalf("SetNestedSlice failed: %v", err)
	}
	return topo
}

func TestParsePodLinks_MTU(t *testing.T) {
	pod := createFakePodTopology("p1", "default", "10.0.0.1", "/proc/1/ns/net", []string{"p2", "p3"})
	withLinkMTU(t, pod, 2, 9216)
	links, err := parsePodLinks(pod)
	if err != nil {
		t.Fatalf("parsePodLinks failed: %v", err)
	}
	if len(links) != 2 {
		t.Fatalf("expected 2 links, got %d", len(links))
	}
	if links[0].MTU != 0 {
		t.Errorf("MTU of link without mtu = %d, want 0", links[0].MTU)
	}
	if links[1].MTU != 9216 {
		t.Errorf("MTU of link with mtu = %d, want 9216", links[1].MTU)
	}
}

func TestPeerLinkMTU(t *testing.T) {
	peer := createFakePodTopology("p2", "default", "10.0.0.2", "/proc/1/ns/net", []string{"p1", "p3"})
	withLinkMTU(t, peer, 1, 9500)
	tests := []struct {
		desc string
		topo *unstructured.Unstructured
		uid  int64
		want int
	}{
		{desc: "set", topo: peer, uid: 1, want: 9500},
		{desc: "unset", topo: peer, uid: 2, want: 0},
		{desc: "missing link", topo: peer, uid: 7, want: 0},
		{desc: "nil topology", uid: 1, want: 0},
	}
	for _, tt := range tests {
		t.Run(tt.desc, func(t *testing.T) {
			if got := peerLinkMTU(tt.topo, tt.uid); got != tt.want {
				t.Errorf("peerLinkMTU(uid %d) = %d, want %d", tt.uid, got, tt.want)
			}
		})
	}
}

func TestRemoteWireMTU(t *testing.T) {
	InitLogger()
	t.Setenv("LINK_MTU", "")
	tests := []struct {
		desc              string
		localMTU, peerMTU int64
		linkMTUEnv        string
		uid               int64
		want              int
	}{
		{desc: "unset", uid: 1, want: 0},
		{desc: "local end", localMTU: 9216, uid: 1, want: 9216},
		{desc: "peer end", peerMTU: 9216, uid: 1, want: 9216},
		{desc: "larger wins", localMTU: 1500, peerMTU: 9500, uid: 1, want: 9500},
		{desc: "LINK_MTU", linkMTUEnv: "9000", uid: 1, want: 9000},
		{desc: "unknown link uses LINK_MTU", localMTU: 9216, linkMTUEnv: "9000", uid: 7, want: 9000},
	}
	for _, tt := range tests {
		t.Run(tt.desc, func(t *testing.T) {
			t.Setenv("LINK_MTU", tt.linkMTUEnv)
			local := createFakePodTopology("p1", "default", "10.0.0.1", "/proc/1/ns/net", []string{"p2"})
			peer := createFakePodTopology("p2", "default", "10.0.0.2", "/proc/1/ns/net", []string{"p1"})
			if tt.localMTU != 0 {
				withLinkMTU(t, local, 1, tt.localMTU)
			}
			if tt.peerMTU != 0 {
				withLinkMTU(t, peer, 1, tt.peerMTU)
			}
			m := &Meshnet{topoCache: NewTopologyCache()}
			m.topoCache.Put(local)
			m.topoCache.Put(peer)

			got := m.remoteWireMTU(context.Background(), &mpb.WireDef{
				LocalPodName: "p1",
				TopoNs:       "default",
				LinkUid:      tt.uid,
			})
			if got != tt.want {
				t.Errorf("remoteWireMTU() = %d, want %d", got, tt.want)
			}
		})
	}
}
