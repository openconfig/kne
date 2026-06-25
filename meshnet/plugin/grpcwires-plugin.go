package main

import (
	"context"
	"fmt"
	"net"
	"strings"
	"time"

	"github.com/containernetworking/plugins/pkg/ns"
	mpb "github.com/networkop/meshnet-cni/daemon/proto/meshnet/v1beta1"
	"github.com/networkop/meshnet-cni/utils/wireutil"
	koko "github.com/redhat-nfvpe/koko/api"
	log "github.com/sirupsen/logrus"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
)

const (
	skipStatusRetryInterval  = 2                            // sec
	skipStatusRetryWarnCount = 5                            // generate a warning while continuing further
	skipStatusRetryCount     = skipStatusRetryWarnCount * 4 // how many times to retry
)

// --------------------------------------------------------------------------------------------------------
func CreatGRPCChan(link *mpb.Link, localPod *mpb.Pod, peerPod *mpb.Pod, localClient mpb.LocalClient, cniArgs *k8sArgs, ctx context.Context) error {
	// At this point pods attached to both end of this link are both up. They have got the management IP already.

	if link == nil {
		return fmt.Errorf("Add-GRPC[%s]: can't establish grpc channel. link not provided. link:%p", localPod.Name, link)
	}

	log.Infof("Add-GRPC[%s]: Setting up grpc-wire:(local-pod:%s:%s@node:%s <----link uid: %d----> remote-pod:%s:%s@node:%s)",
		localPod.Name, localPod.Name, link.LocalIntf, localPod.SrcIp,
		link.Uid, peerPod.Name, link.PeerIntf, peerPod.SrcIp)

	log.Infof("Add-GRPC[%s]: Checking if we've been skipped for link id %d", localPod.Name, link.Uid)
	isSkipped, err := localClient.IsSkipped(ctx, &mpb.SkipQuery{
		Pod:    localPod.Name,
		Peer:   peerPod.Name,
		LinkId: link.Uid,
		KubeNs: string(cniArgs.K8S_POD_NAMESPACE),
	})

	if err != nil {
		log.Errorf("Add-GRPC[%s]: Failed to read skipped status with peer pod %s", localPod.Name, peerPod.Name)
		return err
	}

	wireDef := mpb.WireDef{
		LocalPodNetNs: localPod.NetNs,
		LinkUid:       link.Uid,
		TopoNs:        localPod.KubeNs,
	}
	// Comparing names to determine higher priority
	higherPrio := localPod.Name > peerPod.Name

	if !isSkipped.Response && !higherPrio {
		/*  If peer POD skipped us (booted before us) or we have a higher priority then we initiate the tunnel.
		If peer POD has not skipped us (that means yet to boot or just booted) and it has higher priority
		then we do not initiate the grpc tunnel. When the high priority peer pod boots up (or get ready) then
		it will take care of grpc tunnel creation. This is needed to avoid the race condition when both
		the pods are alive, no one has skipped each other and both of them tries to create the tunnel. In
		this situation only high priority pod must create the tunnel and not the low priority one. This will
		avoid conflict. */

		ticker := time.NewTicker(time.Second * skipStatusRetryInterval)
		defer ticker.Stop()

		iteration := 1
		for range ticker.C {
			// Check if it has created the wire while we were waiting
			resp, err := localClient.GRPCWireExists(ctx, &wireDef)
			if err != nil {
				return fmt.Errorf("Add-GRPC[%s]: could not check grpc wire, %s@%s: %v", localPod.Name, localPod.Name, peerPod.Name, err)
			}
			if resp.Response {
				/* Higher priority pod has created the grpc-link.  */
				log.Infof("Add-GRPC[%s]: grpc wire is already created by the remote peer. Local interface id:%d, local pod %s, peer pod %s", localPod, resp.PeerIntfId, localPod.Name, peerPod.Name)
				return nil
			}

			log.Infof("Add-GRPC[%s]: Retrying to read skipped status for pod %s", localPod.Name, localPod.Name)
			isSkipped, err = localClient.IsSkipped(ctx, &mpb.SkipQuery{
				Pod:    localPod.Name,
				Peer:   peerPod.Name,
				LinkId: link.Uid,
				KubeNs: string(cniArgs.K8S_POD_NAMESPACE),
			})
			if err != nil {
				log.Errorf("Add-GRPC[%s]: Failed to read skipped status from peer pod %s", localPod.Name, peerPod.Name)
				return err
			}

			if !isSkipped.Response {
				if iteration > skipStatusRetryWarnCount {
					log.Warnf("Add-GRPC[%s]: Local pod %s is taking longer time (retry %d) to read skip status, skipped by peer %s.", localPod.Name, localPod.Name, iteration, peerPod.Name)
					if iteration == skipStatusRetryCount {
						log.Infof("Add-GRPC[%s]: Pod %s is not skipped by higher priority pod %s. Link between %s and %s will be created by higher priority pod.",
							localPod.Name, localPod.Name, peerPod.Name, localPod.Name, peerPod.Name)
						return nil
					}
				}
				iteration++
			} else {
				log.Infof("Add-GRPC[%s]: Local pod %s is skipped by peer %s. So we can create wire now", localPod.Name, localPod.Name, peerPod.Name)
				break
			}
		} // end of for
	}

	resp, err := localClient.GRPCWireExists(ctx, &wireDef)
	if err != nil {
		return fmt.Errorf("Add-GRPC[%s]: could not check grpc wire, %s@%s: %v", localPod.Name, localPod.Name, peerPod.Name, err)
	}
	if resp.Response {
		/* While this pod was busy creating other links or was busy with some other task, the remote
		   pod had finished creating this grpc-link.  */
		log.Infof("Add-GRPC[%s]: grpc wire is already set by the remote peer. Local interface id:%d", localPod.Name, resp.PeerIntfId)
		return nil
	}

	// Create the local end of the grpc-wire
	currNs, err := ns.GetCurrentNS()
	if err != nil {
		return fmt.Errorf("Add-GRPC[%s]: creating GRPC wire for pod %s : failed to get node ns, err: %v", localPod.Name, localPod.Name, err)
	}

	// Build koko's veth struct for the intf to be placed inside the pod
	inConIntfNm := link.LocalIntf
	inContainerVeth, err := makeVeth(localPod.NetNs, inConIntfNm, link.LocalIp)
	if err != nil {
		log.Errorf("Add-GRPC[%s]: Could not create vEth for local pod %s:%s, peer pod %s, err %v", localPod.Name, localPod.Name, inConIntfNm, peerPod.Name, err)
		return err
	}

	respIntfName, err := localClient.GenerateNodeInterfaceName(ctx, &mpb.GenerateNodeInterfaceNameRequest{PodIntfName: link.LocalIntf, PodName: localPod.Name})
	if err != nil {
		return fmt.Errorf("Add-GRPC[%s]: could not create node interface for local pod %s, peer pod %s: %v", localPod.Name, err, localPod.Name, peerPod.Name)
	}

	hostEndVeth := &koko.VEth{
		LinkName: respIntfName.NodeIntfName,
		NsName:   currNs.Path()}

	if err = koko.MakeVeth(*inContainerVeth, *hostEndVeth); err != nil {
		return fmt.Errorf("Add-GRPC[%s]: creating GRPC wire: failed to create vEth-pair inside pod (%s:%s) and on host (%s). err:%s",
			localPod.Name, localPod.Name, inContainerVeth.LinkName, hostEndVeth.LinkName, err)
	}

	/* Dial the remote peer to create the remote end of the grpc tunnel. */

	url := fmt.Sprintf("%s:%d", peerPod.SrcIp, wireutil.GRPCDefaultPort)
	url = strings.TrimSpace(url)
	remote, err := grpc.Dial(url, grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		return fmt.Errorf("Add-GRPC[%s]: creating GRPC wire: failed to dial remote gRPC url %s", localPod.Name, url)
	}
	remoteClient := mpb.NewRemoteClient(remote)
	locInf, err := net.InterfaceByName(hostEndVeth.LinkName)
	if err != nil {
		return fmt.Errorf("Add-GRPC[%s]: could not get interface by name: %v", localPod.Name, err)
	}

	wireDefRemot := mpb.WireDef{
		/*WireIfIdOnPeerNode : is the interface id on which a node receives grpc
		packets from the remote pod (hosted in a remote node). Through this interface
		the remote packets are delivered to the local pod.

		WireIfIdOnPeerNode is the interface that will be used to send packets to the remote pod also.
		Packets coming from local pods will be received on this interface, will be encapsulated
		in grpc to send it to the peer node (that is hosting the remote pod).

		The remote pod must send packets to this interface for this grpc-wire, to reach
		the connected container in this node. For remote pod this must be the destination
		interface id to reach the connected pod in this node. From remote pods perspective,
		the local interface of this node is the "PeerIntfId" for the remote pod in remote machine */
		WireIfIdOnPeerNode: int64(locInf.Index),

		/* PeerIp: Ip address of the peer machine/node.
		   For remote pod this must be the IP address on this host. The remote pod must
		   Transport packets to this pod (over grpc) in this local node. This is the IP
		   address of the local node which remote node will do a grpc dial, to send
		   packets over grpc wire. */
		PeerNodeIp: localPod.SrcIp,

		/* We need to tell the remote node, what is the kne specified in container interface name.
		   We also need to tell to which network namespace the pod in remote node belongs to. */
		IntfNameInPod: link.PeerIntf,
		LocalPodNetNs: peerPod.NetNs,
		LocalPodName:  peerPod.Name, // name of the remote pod

		/*meshnet assigned unique identifier for this link */
		LinkUid:    link.Uid,
		TopoNs:     peerPod.KubeNs,
		LocalPodIp: link.PeerIp,
	}

	log.Infof("Add-GRPC[%s]: Create GRPC wire: dialing remote node-->%s@%s", localPod.Name, peerPod.Name, url)
	creatResp, err := remoteClient.AddGRPCWireRemote(ctx, &wireDefRemot)
	if err != nil {
		return fmt.Errorf("Add-GRPC[%s]: failed to create grpc tunnel ar remote end:%s  err:%v", localPod.Name, url, err)
	} else if !creatResp.Response {
		return fmt.Errorf("Add-GRPC[%s]: remote end of the grpc-wire (local-pod:%s:%s@node:%s <----link uid: %d----> remote-pod:%s:%s@node:%s) is not up",
			localPod.Name, localPod.Name, link.LocalIntf, localPod.SrcIp,
			link.Uid, peerPod.Name, link.PeerIntf, peerPod.SrcIp)
	}

	/* remote has finished its job. Register local end of the grpc wire with the daemon
	   and start the packet sending thread. */
	wireDefLocal := mpb.WireDef{
		/*PeerIntfId : this is the interface id (in the remote machine) to which the host/local machine will send grpc
		  packets for the remote pod. This interface id will be encoded in every packet
		  sent over this grpc-wire. This interface id is created in the remote machine and
		  communicated by the remote machine. Availability of this interface id indicates remote
		  machine is ready to receive packets over this grpc-wire. Remote machine will use this
		  interface id to pass the packets to the remote pod. */
		WireIfIdOnPeerNode: creatResp.PeerIntfId,

		/* PeerIp : Ip address of the remote node, to which this local node is sending packets over
		   this grpc-wire.
		*/
		PeerNodeIp: peerPod.SrcIp,

		/* WireIfNameOnLocalNode : name of the local machine interface, from where packets generated by the local
		   pod will be picked up and transported over grpc to remote. local meshnet daemon will receive
		   packets from local pod on this interface.
		*/
		WireIfNameOnLocalNode: respIntfName.NodeIntfName,

		/*meshnet assigned unique identifier for this link */
		LinkUid:       link.Uid,
		LocalPodName:  localPod.Name,
		IntfNameInPod: link.LocalIntf,
		LocalPodNetNs: localPod.NetNs,
		LocalPodIp:    link.LocalIp,
		TopoNs:        localPod.KubeNs,
	}
	log.Infof("Add-GRPC[%s]: Creating GRPC wire: adding the local end of the grpc tunnel.", localPod.Name)
	r, err := localClient.AddGRPCWireLocal(ctx, &wireDefLocal)
	if err != nil {
		return fmt.Errorf("Add-GRPC[%s]: failed to create local end of the tunnel %v", localPod.Name, err)
	} else if !r.Response {
		return fmt.Errorf("Add-GRPC[%s]: local end of the grpc-wire (local-pod:-%s:%s@node:%s <----link uid: %d----> remote-pod:-%s:%s@node:%s) is not up",
			localPod.Name, localPod.Name, link.LocalIntf, localPod.SrcIp,
			link.Uid, peerPod.Name, link.PeerIntf, peerPod.SrcIp)
	}

	log.Infof("Add-GRPC[%s]: Successfully created grpc-wire (local-pod:%s:%s@node:%s:%d <----link uid: %d----> remote-pod:%s:%s@node:%s:%d)",
		localPod.Name, localPod.Name, link.LocalIntf, localPod.SrcIp, locInf.Index,
		link.Uid, peerPod.Name, link.PeerIntf, peerPod.SrcIp, creatResp.PeerIntfId)

	return nil
}

// This function is called when a K8S pod is getting deleted.
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
	removeResp, err := remoteClient.GRPCWireDownRemote(ctx, &wireDefRemot)
	if err != nil {
		return fmt.Errorf("MakeGRPCChanDown: GRPC communication error for : %s, err:%v", url, err)
	} else if !removeResp.Response {
		return fmt.Errorf("MakeGRPCChanDown: remote end of the grpc-wire (local-pod:%s:%s@node:%s <----link uid: %d----> remote-pod:%s:%s@node:%s) is not down",
			localPod.Name, link.LocalIntf, localPod.SrcIp,
			link.Uid, peerPod.Name, link.PeerIntf, peerPod.SrcIp)
	}

	return nil
}
