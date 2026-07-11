package main

import (
	"context"
	"encoding/json"
	"fmt"
	"net"
	"os"
	"runtime"

	"github.com/containernetworking/cni/pkg/skel"
	"github.com/containernetworking/cni/pkg/types"
	"github.com/containernetworking/cni/pkg/types/current"
	"github.com/containernetworking/cni/pkg/version"
	koko "github.com/redhat-nfvpe/koko/api"
	log "github.com/sirupsen/logrus"
	"github.com/vishvananda/netlink"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"

	mpb "github.com/openconfig/kne/third_party/meshnet/daemon/proto/meshnet/v1beta1"
	"github.com/openconfig/kne/third_party/meshnet/utils/wireutil"
)

const (
	vxlanBase   = 5000
	localhost   = "localhost"
	macvlanMode = netlink.MACVLAN_MODE_BRIDGE
)

var (
	localDaemon = localhost + ":" + fmt.Sprintf("%d", wireutil.GRPCDefaultPort)
)

var interNodeLinkType = wireutil.INTER_NODE_LINK_VXLAN

type netConf struct {
	types.NetConf
	Delegate map[string]interface{} `json:"delegate"`
}

type k8sArgs struct {
	types.CommonArgs
	K8S_POD_NAME               types.UnmarshallableString
	K8S_POD_NAMESPACE          types.UnmarshallableString
	K8S_POD_INFRA_CONTAINER_ID types.UnmarshallableString
}

// -------------------------------------------------------------------------------------------------
func init() {
	// this ensures that main runs only on main thread (thread group leader).
	// since namespace ops (unshare, setns) are done for a single thread, we
	// must ensure that the goroutine does not jump from OS thread to thread
	runtime.LockOSThread()
}

// -------------------------------------------------------------------------------------------------
// loadConf loads information from cni.conf
func loadConf(bytes []byte) (*netConf, *current.Result, error) {
	n := &netConf{}
	if err := json.Unmarshal(bytes, n); err != nil {
		return nil, nil, fmt.Errorf("failed to load netconf: %v", err)
	}

	// Parse previous result.
	if n.RawPrevResult == nil {
		// return early if there was no previous result, which is allowed for DEL calls
		return n, &current.Result{}, nil
	}

	// Parse previous result.
	var result *current.Result
	var err error
	if err = version.ParsePrevResult(&n.NetConf); err != nil {
		return nil, nil, fmt.Errorf("could not parse prevResult: %v", err)
	}

	result, err = current.NewResultFromResult(n.PrevResult)
	if err != nil {
		return nil, nil, fmt.Errorf("could not convert result to current version: %v", err)
	}
	return n, result, nil
}

// getVxlanSource uses netlink to get the iface reliably given an IP address.
// when IP and Interface both are present then Interface is going to take preference
// nodeIntf is specified by the user and it's not auto discovered. The user has to be careful that the peer is reachable though this interface otherwise, VxLAN may not work.
// daemonset.yaml meshnet container env required for host_intf override
//
//	       env:
//	         - name: HOST_INTF
//			value: breth2
func getVxlanSource(nodeIP string, nodeIntf string) (string, string, error) {
	if nodeIntf == "" && nodeIP == "" {
		return "", "", fmt.Errorf("meshnetd provided no HOST_IP address: %s or HOST_INTF: %s", nodeIP, nodeIntf)
	}
	nIP := net.ParseIP(nodeIP)
	if nIP == nil && nodeIntf == "" {
		return "", "", fmt.Errorf("parsing failed for meshnetd provided no HOST_IP address: %s and node HOST_INTF: %s", nodeIP, nodeIntf)
	}
	ifaces, _ := net.Interfaces()
	for _, i := range ifaces {
		addrs, _ := i.Addrs()
		for _, a := range addrs {
			var ip net.IP
			switch v := a.(type) {
			case *net.IPNet:
				ip = v.IP
			case *net.IPAddr:
				ip = v.IP
			}
			if nodeIntf != "" {
				if i.Name == nodeIntf {
					return ip.String(), nodeIntf, nil
				}
			}
			if nIP != nil && nIP.Equal(ip) {
				log.Infof("Found iface %s for address %s", i.Name, nodeIP)
				return nodeIP, i.Name, nil
			}
		}
	}
	return "", "", fmt.Errorf("no iface found for address %s", nodeIP)
}

// -------------------------------------------------------------------------------------------------
// makeVeth creates koko.Veth from NetNS and LinkName
func makeVeth(netNS, linkName string, ip string) (*koko.VEth, error) {
	log.Infof("Creating Veth struct with NetNS:%s and intfName: %s, IP:%s", netNS, linkName, ip)
	veth := koko.VEth{}
	veth.NsName = netNS
	veth.LinkName = linkName
	if ip != "" {
		ipAddr, ipSubnet, err := net.ParseCIDR(ip)
		if err != nil {
			return nil, fmt.Errorf("failed to parse CIDR %s: %s", ip, err)
		}
		veth.IPAddr = []net.IPNet{{
			IP:   ipAddr,
			Mask: ipSubnet.Mask,
		}}
	}
	return &veth, nil
}

// -------------------------------------------------------------------------------------------------
// Creates koko.Vxlan from ParentIF, destination IP and VNI
func makeVxlan(srcIntf string, peerIP string, idx int64) *koko.VxLan {
	return &koko.VxLan{
		ParentIF: srcIntf,
		IPAddr:   net.ParseIP(peerIP),
		ID:       int(vxlanBase + idx),
	}
}

// -------------------------------------------------------------------------------------------------
// Adds interfaces to a POD. the POD name and the POD Namespace is passed as arguments.
func cmdAdd(args *skel.CmdArgs) error {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	log.Info("Parsing cni .conf file")
	n, result, err := loadConf(args.StdinData)
	if err != nil {
		return err
	}

	log.Info("Parsing CNI_ARGS environment variable")
	cniArgs := k8sArgs{}
	if err := types.LoadArgs(args.Args, &cniArgs); err != nil {
		return err
	}
	log.Infof("Processing ADD POD %s in namespace %s, container_id %s", string(cniArgs.K8S_POD_NAME), string(cniArgs.K8S_POD_NAMESPACE), string(cniArgs.K8S_POD_INFRA_CONTAINER_ID))
	defer log.Infof("DONE > Processing ADD POD %s in namespace %s, container_id %s", string(cniArgs.K8S_POD_NAME), string(cniArgs.K8S_POD_NAMESPACE), string(cniArgs.K8S_POD_INFRA_CONTAINER_ID))

	log.Infof("Attempting to connect to local meshnet daemon")
	conn, err := grpc.Dial(localDaemon, grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		log.Errorf("Failed to connect to local meshnetd on %s", localDaemon)
		return err
	}
	defer conn.Close()

	meshnetClient := mpb.NewLocalClient(conn)

	log.Infof("Add[%s]: Retrieving local pod information from meshnet daemon", string(cniArgs.K8S_POD_NAME))
	localPod, err := meshnetClient.Get(ctx, &mpb.PodQuery{
		Name:   string(cniArgs.K8S_POD_NAME),
		KubeNs: string(cniArgs.K8S_POD_NAMESPACE),
	})
	if err != nil {
		log.Errorf("Add: Pod %s:%s was not a topology pod returning", string(cniArgs.K8S_POD_NAMESPACE), string(cniArgs.K8S_POD_NAME))
		return types.PrintResult(result, n.CNIVersion)
	}

	// Finding the source IP and interface for VXLAN VTEP
	srcIP, srcIntf, err := getVxlanSource(localPod.NodeIp, localPod.NodeIntf)
	if err != nil {
		return err
	}
	_ = srcIntf
	//log.Infof("VxLan route is via %s@%s", srcIP, srcIntf)

	// Marking pod as "alive" by setting its srcIP and NetNS and ContainerId
	localPod.NetNs = args.Netns
	localPod.SrcIp = srcIP
	localPod.ContainerId = args.ContainerID
	log.Infof("Add[%s]: Setting pod alive status on meshnet daemon", string(cniArgs.K8S_POD_NAME))
	ok, err := meshnetClient.SetAlive(ctx, localPod)
	if err != nil || !ok.Response {
		log.Errorf("Add[%s]: Failed to set pod alive status", string(cniArgs.K8S_POD_NAME))
		return err
	}

	log.Infof("Add[%s]: Successfully registered pod alive status with meshnet daemon", string(cniArgs.K8S_POD_NAME))
	return types.PrintResult(result, n.CNIVersion)
}

// -------------------------------------------------------------------------------------------------
// Deletes interfaces from a POD
func cmdDel(args *skel.CmdArgs) error {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	cniArgs := k8sArgs{}
	if err := types.LoadArgs(args.Args, &cniArgs); err != nil {
		return err
	}
	log.Infof("========> Processing DEL POD %s in namespace %s, container_id %s", string(cniArgs.K8S_POD_NAME), string(cniArgs.K8S_POD_NAMESPACE), cniArgs.K8S_POD_INFRA_CONTAINER_ID)
	defer log.Infof("DONE ========>  Processing DEL POD %s in namespace %s, container_id %s", string(cniArgs.K8S_POD_NAME), string(cniArgs.K8S_POD_NAMESPACE), cniArgs.K8S_POD_INFRA_CONTAINER_ID)

	log.WithFields(log.Fields{
		"args": fmt.Sprintf("%+v", args),
	}).Info("DEL request arguments")
	log.Info("Parsing cni .conf file")
	n, result, err := loadConf(args.StdinData)
	if err != nil {
		return err
	}

	conn, err := grpc.Dial(localDaemon, grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		log.Errorf("Failed to connect to local meshnetd on %s", localDaemon)
		return err
	}
	defer conn.Close()

	meshnetClient := mpb.NewLocalClient(conn)

	log.Infof("Del: Retrieving pod's (%s@%s) metadata from meshnet daemon", string(cniArgs.K8S_POD_NAME), string(cniArgs.K8S_POD_NAMESPACE))
	localPod, err := meshnetClient.Get(ctx, &mpb.PodQuery{
		Name:   string(cniArgs.K8S_POD_NAME), // getting details of the current pod.
		KubeNs: string(cniArgs.K8S_POD_NAMESPACE),
	})
	if err != nil {
		log.Infof("Del: Pod %s:%s is not in topology returning. err:%v", string(cniArgs.K8S_POD_NAMESPACE), string(cniArgs.K8S_POD_NAME), err)
		return types.PrintResult(result, n.CNIVersion)
	}
	// if the current containerID is not the topo's current containerID, exit here b/c this is a duplicated DEL call.
	if localPod.ContainerId != args.ContainerID {
		log.Infof("Del: Pod %s:%s is a duplicate; skipping", string(cniArgs.K8S_POD_NAMESPACE), string(cniArgs.K8S_POD_NAME))
		return nil
	}

	/* Tell daemon to close the grpc tunnel for this pod netns (if any) */
	log.Infof("Del: Retrieving pod's metadata from meshnet daemon")
	wireDef := mpb.WireDef{
		TopoNs:       string(cniArgs.K8S_POD_NAMESPACE),
		LocalPodName: string(cniArgs.K8S_POD_NAME),
	}

	removResp, err := meshnetClient.RemGRPCWire(ctx, &wireDef)
	if err != nil || !removResp.Response {
		return fmt.Errorf("del: could not remove grpc wire: %v", err)
	}

	localPodSrcIp := localPod.SrcIp
	log.Infof("Del: Topology data still exists in CRs, cleaning up it's status")
	// By setting srcIP and NetNS to "" we're marking this POD as dead
	localPod.NetNs = ""
	localPod.SrcIp = ""
	localPod.ContainerId = ""
	_, err = meshnetClient.SetAlive(ctx, localPod)
	if err != nil {
		return fmt.Errorf("del: could not set alive: %v", err)
	}

	log.Infof("Del: Iterating over each link for clean-up")
	for _, link := range localPod.Links { // Iterate over each link of the local pod
		linkType := "veth"
		// Initialising peer pod's metadata. Peer pod information is needed to determine if a link is of type GRPC or VxLAN.
		// For GRPC link type there is some additional cleanup work needs to be performed.
		log.Infof("Del: Pod %s is retrieving peer pod %s information from meshnet daemon", string(cniArgs.K8S_POD_NAME), link.PeerPod)
		peerPod, _ := meshnetClient.Get(ctx, &mpb.PodQuery{
			Name:   link.PeerPod, // getting peer pod detail.
			KubeNs: string(cniArgs.K8S_POD_NAMESPACE),
		})

		if peerPod.SrcIp != localPodSrcIp {
			// they are on different hosts
			if interNodeLinkType == wireutil.INTER_NODE_LINK_GRPC {
				// for this link bring the grpc wire down
				err = MakeGRPCChanDown(link, localPod, peerPod, ctx)
				if err != nil {
					log.Errorf("Del: !! Failed to remove remote grpc wire. err: %v", err)
					// no return
				}
				linkType = "grpc"
			} else {
				linkType = "vxlan"
			}
		}
		// Creating koko's Veth struct for local intf
		myVeth, err := makeVeth(args.Netns, link.LocalIntf, link.LocalIp)
		if err != nil {
			log.Infof("Del: Failed to construct koko Veth struct")
			return err
		}

		log.Infof("Del: Removing link %s", link.LocalIntf)
		// API call to koko to remove local Veth link
		if err = myVeth.RemoveVethLink(); err != nil {
			// instead of failing, just log the error and move on
			log.Errorf("Del: Error removing Veth link %s (%s) on pod %s: %v", link.LocalIntf, linkType, localPod.Name, err)
		}

		// Setting reversed skipped flag so that this pod will try to connect veth pair on restart
		log.Infof("Del: Setting skip-reverse flag on peer %s@%s(link id %d) for local interface %s", link.PeerPod, link.PeerIntf, link.Uid, link.LocalIntf)
		ok, err := meshnetClient.SkipReverse(ctx, &mpb.SkipQuery{
			Pod:    localPod.Name,
			Peer:   link.PeerPod,
			LinkId: link.Uid,
			KubeNs: string(cniArgs.K8S_POD_NAMESPACE),
		})
		if err != nil || !ok.Response {
			log.Errorf("Del: Failed to set skip reversed flag on our peer %s", link.PeerPod)
			return err
		}
	}
	return nil
}

func SetInterNodeLinkType() {
	// TODO: Find a more appropriate (if any) way to figure out intended link type
	// As of today, daemon gets the intended link type from env INTER_NODE_LINK_TYPE
	// which is set by deployment file. The daemon further propagates this to plugin
	// via means of file on host (which is read below) containing the value GRPC or VXLAN
	b, err := os.ReadFile("/etc/cni/net.d/meshnet-inter-node-link-type")
	if err != nil {
		log.Warningf("Could not read iner node link type: %v", err)
		// use the default value
		return
	}

	interNodeLinkType = string(b)
}

// -------------------------------------------------------------------------------------------------
func main() {
	fp, err := os.OpenFile("/var/log/meshnet-cni.log", os.O_APPEND|os.O_CREATE|os.O_RDWR, 0666)
	if err == nil {
		log.SetOutput(fp)
	}

	SetInterNodeLinkType()
	log.Infof("INTER_NODE_LINK_TYPE: %v", interNodeLinkType)

	retCode := 0
	e := skel.PluginMainWithError(cmdAdd, cmdGet, cmdDel, version.All, "CNI plugin meshnet v0.3.0")
	if e != nil {
		log.Errorf("failed to run meshnet cni: %v", e.Print())
		retCode = 1
	}
	log.Infof("K8S invoked meshnet cni")
	fp.Close()
	os.Exit(retCode)
}

func cmdGet(args *skel.CmdArgs) error {
	cniArgs := k8sArgs{}
	if err := types.LoadArgs(args.Args, &cniArgs); err != nil {
		return err
	}
	log.Infof("cmdGet called: %+v", args)
	return fmt.Errorf("not implemented")
}

//-------------------------------------------------------------------------------------------------
