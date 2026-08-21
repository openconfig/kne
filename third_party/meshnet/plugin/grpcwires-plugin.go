package main

import (
	"context"
	"fmt"
	"strings"
	"time"

	mpb "github.com/openconfig/kne/third_party/meshnet/daemon/proto/meshnet/v1beta1"
	"github.com/openconfig/kne/third_party/meshnet/utils/wireutil"
	log "github.com/sirupsen/logrus"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
)

// MakeGRPCChanDown signals the remote peer node to tear down the remote gRPC wire end when a pod is deleted.
func MakeGRPCChanDown(link *mpb.Link, localPod *mpb.Pod, peerPod *mpb.Pod, ctx context.Context) error {
	if link == nil {
		return fmt.Errorf("can't remove remote grpc info. link not provided. link:%p", link)
	}

	/* Dial the remote peer to bring down the remote grpc wire end */

	url := fmt.Sprintf("%s:%d", peerPod.SrcIp, wireutil.GRPCDefaultPort)
	url = strings.TrimSpace(url)
	remote, err := grpc.Dial(url, grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		return fmt.Errorf("MakeGRPCChanDown failed to dial remote gRPC url %s", url)
	}
	defer remote.Close()
	remoteClient := mpb.NewRemoteClient(remote)

	wireDefRemot := mpb.WireDef{
		PeerNodeIp:    localPod.SrcIp, // for remote pod this pod is the peer pod
		IntfNameInPod: link.PeerIntf,
		LocalPodNetNs: peerPod.NetNs,
		LocalPodName:  peerPod.Name,

		/*meshnet assigned unique identifier for this link */
		LinkUid:    link.Uid,
		TopoNs:     peerPod.KubeNs,
		LocalPodIp: link.PeerIp,
	}

	log.Infof("MakeGRPCChanDown: dialing remote node-->%s@%s", peerPod.Name, url)
	rpcCtx, rpcCancel := context.WithTimeout(ctx, 5*time.Second)
	defer rpcCancel()
	removeResp, err := remoteClient.GRPCWireDownRemote(rpcCtx, &wireDefRemot)
	if err != nil {
		return fmt.Errorf("MakeGRPCChanDown: GRPC communication error for : %s, err:%v", url, err)
	} else if !removeResp.Response {
		return fmt.Errorf("MakeGRPCChanDown: remote end of the grpc-wire (local-pod:%s:%s@node:%s <----link uid: %d----> remote-pod:%s:%s@node:%s) is not down",
			localPod.Name, link.LocalIntf, localPod.SrcIp,
			link.Uid, peerPod.Name, link.PeerIntf, peerPod.SrcIp)
	}

	return nil
}

