package meshnet

import (
	"context"
	"testing"

	fakeTopology "github.com/openconfig/kne/third_party/meshnet/api/clientset/v1beta1/fake"
	topologyv1 "github.com/openconfig/kne/third_party/meshnet/api/types/v1beta1"
	mpb "github.com/openconfig/kne/third_party/meshnet/daemon/proto/meshnet/v1beta1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func TestGet_NoLinks(t *testing.T) {
	InitLogger()

	topoNoLinks := &topologyv1.Topology{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "dut1",
			Namespace: "default",
		},
		Spec: topologyv1.TopologySpec{
			Links: nil,
		},
		Status: topologyv1.TopologyStatus{
			SrcIP: "/proc/1/ns/net",
		},
	}

	fakeClient, err := fakeTopology.NewSimpleClientset(topoNoLinks)
	if err != nil {
		t.Fatalf("failed to create fake topology clientset: %v", err)
	}
	m := &Meshnet{
		tClient: fakeClient,
	}

	pod, err := m.Get(context.Background(), &mpb.PodQuery{
		Name:   "dut1",
		KubeNs: "default",
	})
	if err != nil {
		t.Fatalf("Get failed for pod without links: %v", err)
	}
	if pod == nil {
		t.Fatalf("expected non-nil pod, got nil")
	}
	if len(pod.Links) != 0 {
		t.Fatalf("expected 0 links, got %d", len(pod.Links))
	}
	if pod.Name != "dut1" {
		t.Fatalf("expected pod name dut1, got %s", pod.Name)
	}
}

func TestGet_WithLinks(t *testing.T) {
	InitLogger()

	topoWithLinks := &topologyv1.Topology{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "dut1",
			Namespace: "default",
		},
		Spec: topologyv1.TopologySpec{
			Links: []topologyv1.Link{
				{
					UID:       1,
					LocalIntf: "eth1",
					PeerIntf:  "eth1",
					PeerPod:   "dut2",
					LocalIP:   "10.0.0.1/30",
					PeerIP:    "10.0.0.2/30",
				},
			},
		},
	}

	fakeClient, err := fakeTopology.NewSimpleClientset(topoWithLinks)
	if err != nil {
		t.Fatalf("failed to create fake topology clientset: %v", err)
	}
	m := &Meshnet{
		tClient: fakeClient,
	}

	pod, err := m.Get(context.Background(), &mpb.PodQuery{
		Name:   "dut1",
		KubeNs: "default",
	})
	if err != nil {
		t.Fatalf("Get failed for pod with links: %v", err)
	}
	if pod == nil {
		t.Fatalf("expected non-nil pod, got nil")
	}
	if len(pod.Links) != 1 {
		t.Fatalf("expected 1 link, got %d", len(pod.Links))
	}
	if pod.Links[0].PeerPod != "dut2" || pod.Links[0].LocalIntf != "eth1" {
		t.Fatalf("unexpected link metadata: %+v", pod.Links[0])
	}
}
