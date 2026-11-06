package meshnet

import (
	"context"

	mpb "github.com/openconfig/kne/third_party/meshnet/daemon/proto/meshnet/v1beta1"
	"github.com/openconfig/kne/third_party/meshnet/utils/wireutil"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
)

// findPodLink returns the link with the given UID in the spec of topo.
func findPodLink(topo *unstructured.Unstructured, uid int64) (wireutil.PodLinkConfig, bool) {
	links, _ := parsePodLinks(topo)
	for _, l := range links {
		if l.LinkUID == uid {
			return l, true
		}
	}
	return wireutil.PodLinkConfig{}, false
}

// peerLinkMTU returns the MTU that the peer pod's Topology requests for the
// link with the given UID, or zero if it requests none.
func peerLinkMTU(peerTopo *unstructured.Unstructured, uid int64) int {
	if l, ok := findPodLink(peerTopo, uid); ok {
		return l.MTU
	}
	return 0
}

// remoteWireMTU returns the MTU for the local end of a grpc-wire that a peer
// node asked this node to create, looked up from the Topology resources of the
// local pod and its peer (see wireutil.LinkMTU).
func (m *Meshnet) remoteWireMTU(ctx context.Context, wireDef *mpb.WireDef) int {
	topo, err := m.getPod(ctx, wireDef.GetLocalPodName(), wireDef.GetTopoNs())
	if err != nil {
		mnetdLogger.Warnf("remoteWireMTU: failed to get topology of pod %s/%s: %v", wireDef.GetTopoNs(), wireDef.GetLocalPodName(), err)
		return wireutil.LinkMTU(0, 0)
	}
	local, ok := findPodLink(topo, wireDef.GetLinkUid())
	if !ok {
		return wireutil.LinkMTU(0, 0)
	}
	peerMTU := 0
	if peerTopo, err := m.getPod(ctx, local.PeerPodName, wireDef.GetTopoNs()); err == nil {
		peerMTU = peerLinkMTU(peerTopo, wireDef.GetLinkUid())
	}
	return wireutil.LinkMTU(local.MTU, peerMTU)
}
