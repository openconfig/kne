package grpcwire

import (
	"context"
	"fmt"
	"net"
	"os"
	"reflect"

	"github.com/google/gopacket/pcap"
	grpcwirev1 "github.com/networkop/meshnet-cni/api/types/v1beta1"
	mpb "github.com/networkop/meshnet-cni/daemon/proto/meshnet/v1beta1"
	log "github.com/sirupsen/logrus"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/fields"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/util/retry"
)

// GWireClient is dynamic client for grpc wire. it is used to read/write grpc wire info from/to k8s api data-store
type GWireClient struct {
	di  dynamic.NamespaceableResourceInterface
	gvr schema.GroupVersionResource
}

var gWClient GWireClient

const (
	kStatus        = "status"        // json name of Status of gwire_type, +++TBD: can we make it dynamic
	kGrpcWireItems = "grpcWireItems" // json name of GWireKItems of gwire_type, +++TBD: can we make it dynamic
)

// -----------------------------------------------------------------------------------------------------------
func SetGWireClient(gClient *dynamic.DynamicClient) {
	// identifier<group, version, resource> of grpc wire object in k8s apis
	gWClient.gvr = schema.GroupVersionResource{
		Group:    grpcwirev1.GroupName,
		Version:  grpcwirev1.GroupVersion,
		Resource: grpcwirev1.GWireResNamePlural,
	}
	gWClient.di = gClient.Resource(gWClient.gvr)
}

// -----------------------------------------------------------------------------------------------------------
func SetGWireClientInterface(gClient dynamic.NamespaceableResourceInterface) {
	gWClient.di = gClient
}

// ------------------------------------------------------------------------------------------------------------
func (gc GWireClient) GetWireObjListUS(ctx context.Context, ndName string) (*unstructured.UnstructuredList, error) {
	return gc.di.Namespace("").List(ctx, metav1.ListOptions{
		TypeMeta: metav1.TypeMeta{
			Kind: reflect.TypeOf(grpcwirev1.GWireKObj{}).Name(),
		},
		FieldSelector: fields.SelectorFromSet(
			fields.Set{metav1.ObjectNameField: ndName}, // need GRPC wire endpoint information for this node only
		).String(),
	})
}

// ------------------------------------------------------------------------------------------------------------
func (gc GWireClient) CreatWireObj(ctx context.Context, nSpace string, uWbj map[string]interface{}) (*unstructured.Unstructured, error) {
	return gc.di.Namespace(nSpace).Create(ctx, &unstructured.Unstructured{Object: uWbj}, metav1.CreateOptions{})
}

// ------------------------------------------------------------------------------------------------------------
func (gc GWireClient) UpdateWireObj(ctx context.Context, nSpace string, wObjsOnNd *unstructured.Unstructured) (*unstructured.Unstructured, error) {
	return gc.di.Namespace(nSpace).Update(ctx, wObjsOnNd, metav1.UpdateOptions{})

}

//------------------------------------------------------------------------------------------------------------

func (gc GWireClient) GetWireObjGrpUS(ctx context.Context, wStatus *grpcwirev1.GWireStatus) (*unstructured.Unstructured, error) {
	return gc.di.Namespace(wStatus.TopoNamespace).Get(ctx, wStatus.LocalNodeName, metav1.GetOptions{})
}

// -----------------------------------------------------------------------------------------------------------
// Create & populate "GWireStatus" from a "GRPCWire". GWireStatus is stored in K8S data-store
func CreateWireStatus(wire *GRPCWire, nodeName string) *grpcwirev1.GWireStatus {

	return &grpcwirev1.GWireStatus{
		LocalNodeName: nodeName,
		LinkId:        int64(wire.UID),
		TopoNamespace: wire.TopoNamespace,

		//local pod information
		LocalPodNetNs:            wire.LocalPodNetNS,
		WireIfaceNameOnLocalNode: wire.LocalNodeIfaceName,
		LocalPodName:             wire.LocalPodName,
		LocalPodIfaceName:        wire.LocalPodIfaceName,
		LocalPodIp:               wire.LocalPodIP,

		//peer information
		WireIfaceIdOnPeerNode: wire.WireIfaceIDOnPeerNode,
		GWirePeerNodeIp:       wire.PeerNodeIP,
	}

}

// -----------------------------------------------------------------------------------------------------------
// K8sStoreGWire writes grpc wire info 'wire' for a specific topology namespace (wire.TopoNamespace) into k8s
// data-store for the current node. It calls updateGRPCWireStatus() to serve the purpose
func (wire *GRPCWire) K8sStoreGWire() error {
	nodeName, err := findNodeName()
	if err != nil {
		grpcOvrlyLogger.Errorf("K8sStoreGWire: could not get node name: %v", err)
		return err
	}

	ctx := context.Background()
	ws := CreateWireStatus(wire, nodeName)
	err = updateGRPCWireStatus(ctx, ws)

	if err != nil {
		grpcOvrlyLogger.Errorf("K8sStoreGWire: Failed to set status for node %s: %v", nodeName, err)
	}
	return nil
}

// -----------------------------------------------------------------------------------------------------------
// K8sDelGWire deletes grpc wire info 'wire' for a specific namespace from k8s api data-store for the current
// node. namespace is specified in given 'wire' argument. it calls deleteGRPCWireStatus() to serve the purpose
func (wire *GRPCWire) K8sDelGWire() error {
	nodeName, err := findNodeName()
	if err != nil {
		grpcOvrlyLogger.Errorf("K8sDelGWire: could not get node name: %v", err)
	}
	ctx := context.Background()

	ws := CreateWireStatus(wire, nodeName)
	err = deleteGRPCWireStatus(ctx, ws)

	if err != nil {
		grpcOvrlyLogger.Errorf("Failed to delete wire status for node %s: %v", nodeName, err)
		return err
	}
	return nil
}

// -----------------------------------------------------------------------------------------------------------
// On meshnet daemon reboot ReconGWires reconciles all grpc wires of all namespaces (topologies) in local memory.
// InK8S data store, it looks for
// - gwireKObj for all name-spaces
//   - iterate over all wire info list present in gwireKObj
//   - call reCreateGWire() with saved wire info to build up the in memory wire map
func ReconGWires() error {
	nodeName, err := findNodeName()
	if err != nil {
		grpcOvrlyLogger.Errorf("ReconGWires: could not get node: %v", err)
		return err
	}

	ctx := context.Background()
	retryErr := retry.RetryOnConflict(retry.DefaultRetry, func() error {
		// retrieve list of grpc wire obj list for all namespaces for the current node-name
		gwireKObjList, err := gWClient.GetWireObjListUS(ctx, nodeName)
		if err != nil {
			grpcOvrlyLogger.Errorf("reconGWires: could not get gWireKObjs from k8s: %v", err)
			return err
		}
		// in the unlikely situation where one has multiple topologies running in the same cluster,
		// gwireKObjList will have multiple items for this node.
		// {(<node-1><topo-namespace-1>),(<node-1><topo-namespace-2>),...}
		for _, node := range gwireKObjList.Items {
			// a node is found and node-Status-GWireKItems exists, so reconcile
			grpcWireItems, found, err := unstructured.NestedSlice(node.Object, kStatus, kGrpcWireItems)
			if err != nil {
				grpcOvrlyLogger.Errorf("ReconGWires: could not retrieve grpcWireItem: %v", err)
				continue
			}
			if !found {
				grpcOvrlyLogger.Errorf("ReconGWires: grpcWireItem not found in GWireKObj status, retrieved from k8s data-store")
				continue
			}
			if grpcWireItems == nil {
				grpcOvrlyLogger.Errorf("ReconGWires: grpcWireItem is nil in GWireKObj status, retrieved from k8s data-store")
				continue
			}
			for _, grpcWireItem := range grpcWireItems {
				wireStatusItem, ok := grpcWireItem.(map[string]interface{})
				if !ok {
					grpcOvrlyLogger.Errorf("ReconGWires: unable to retrieve wire status item, %v is not a map", grpcWireItem)
					continue
				}

				// create the wire structure from the saved data in K8S data store
				wireStatus := grpcwirev1.GWireStatus{}
				if err := runtime.DefaultUnstructuredConverter.FromUnstructured(wireStatusItem, &wireStatus); err != nil {
					grpcOvrlyLogger.Errorf("ReconGWires: unable to retrieve wire status: %v", err)
					continue
				}
				reCreateGWire(wireStatus, ctx)
			}
		}
		return nil
	})
	if retryErr != nil {
		grpcOvrlyLogger.Errorf("Failed to read status on node %s", nodeName)
		return retryErr
	}

	return nil
}

// -----------------------------------------------------------------------------------------------------------
// updateGRPCWireStatus writes grpc wire 'wStatus' into k8s data-store. 'wStatus' for all existing grpc wires are added
// under 'grpcWireItems' as part of status. Status is part of 'GWireKObj' and identified
// by name=<current-node-name>. For the first write, this object for a node does not exist in k8s data-
// store. So for first write, it creates the object and then adds the 'wStatus'. For all subsequent 'wStatus'
// to be added, first get the object from data-store, append the 'wStatus' to the existing list of
// 'grpcWireItems' and write the updated 'grpcWireItems' list back to k8s api data-store.
func updateGRPCWireStatus(ctx context.Context, wStatus *grpcwirev1.GWireStatus) error {
	grpcOvrlyLogger.Infof("Updating GRPC wire status on node %s, pod %s@%s", wStatus.LocalNodeName, wStatus.LocalPodName, wStatus.LocalPodIfaceName)

	retryErr := retry.RetryOnConflict(retry.DefaultRetry, func() error {

		// wires are grouped by node name. retrieve the group for this node 'wStatus.LocalNodeName'
		wObjsOnNd, err := gWClient.GetWireObjGrpUS(ctx, wStatus)
		if err != nil {
			// if not found then create it
			if errors.IsNotFound(err) {
				// WireObj does not exist. add it first
				err = CreateGWireStatInDS(ctx, wStatus)
				if err != nil {
					return err
				}
				grpcOvrlyLogger.Infof("updateGRPCWireStatus: Created node %s, pod %s@%s into k8s data-store",
					wStatus.LocalNodeName, wStatus.LocalPodName, wStatus.LocalPodIfaceName)
				return nil
			}

			return err // for all error expect 'not found' return the error
		}

		// extract status gwire items
		gwireItems, found, err := unstructured.NestedSlice(wObjsOnNd.Object, kStatus, kGrpcWireItems)
		if err != nil {
			grpcOvrlyLogger.Errorf("updateGRPCWireStatus: could not retrieve gWireItems: %v", err)
			return err
		}
		if !found {
			grpcOvrlyLogger.Errorf("updateGRPCWireStatus: gwireItems not found in GWireKObj status")
			return err
		}
		if gwireItems == nil {
			grpcOvrlyLogger.Errorf("updateGRPCWireStatus: gwireItems is nil in GWireKObj status")
			return err
		}
		newItem, err := runtime.DefaultUnstructuredConverter.ToUnstructured(wStatus)
		if err != nil {
			grpcOvrlyLogger.Errorf("updateGRPCWireStatus: could not convert to unstructured: %v\n", err)
			return err
		}
		gwireItems = append(gwireItems, newItem)

		if err := unstructured.SetNestedField(wObjsOnNd.Object, gwireItems, kStatus, kGrpcWireItems); err != nil {
			grpcOvrlyLogger.Errorf("updateGRPCWireStatus: could not set grpcwireitems status: %v", err)
			return err
		}

		_, err = gWClient.UpdateWireObj(ctx, wStatus.TopoNamespace, wObjsOnNd)
		if err != nil {
			grpcOvrlyLogger.Infof("updateGRPCWireStatus: Could not update GRPCWire status for node %s, pod %s@%s into K8s",
				wObjsOnNd.GetName(), wStatus.LocalPodName, wStatus.LocalPodIfaceName)
			return err
		}
		return nil

	})
	if retryErr != nil {
		log.WithFields(log.Fields{
			"daemon":   "meshnetd",
			"err":      retryErr,
			"function": "updateGRPCWireStatus",
		}).Errorf("Failed to update status on node %s, pod %s@%s", wStatus.LocalNodeName, wStatus.LocalPodName, wStatus.LocalPodIfaceName)
		return retryErr
	}

	return nil
}

// -----------------------------------------------------------------------------------------------------------
// CreateGWireStatInDS creates grpc wire unstructured object with gvr info populated in it.
func CreateGWireStatInDS(ctx context.Context, wStatus *grpcwirev1.GWireStatus) error {
	wObj := &grpcwirev1.GWireKObj{
		TypeMeta: metav1.TypeMeta{
			Kind:       reflect.TypeOf(grpcwirev1.GWireKObj{}).Name(),
			APIVersion: grpcwirev1.GroupName + "/" + grpcwirev1.GroupVersion,
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      wStatus.LocalNodeName,
			Namespace: wStatus.TopoNamespace,
		},
		Status: grpcwirev1.GWireKNodeStatus{
			GWireKItems: []grpcwirev1.GWireStatus{*wStatus},
		},
	}
	uWbj, err := runtime.DefaultUnstructuredConverter.ToUnstructured(wObj)
	if err != nil {
		grpcOvrlyLogger.Errorf("CreateGWireStatInDS: could not create unstructured for new wire: %v", err)
		return err
	}

	_, err = gWClient.CreatWireObj(ctx, wStatus.TopoNamespace, uWbj)
	if err != nil {
		grpcOvrlyLogger.Errorf("CreateGWireStatInDS: Could not create node %s, pod %s@%s into k8s data-store: %v",
			wStatus.LocalNodeName, wStatus.LocalPodName, wStatus.LocalPodIfaceName, err)
		return err
	}
	return nil
}

// -----------------------------------------------------------------------------------------------------------
// deleteGRPCWireStatus deletes a grpc wire status from 'grpcWireItems' for a specific namespace
// for this node. Topology namespace is derived from given 'wStatus'.
func deleteGRPCWireStatus(ctx context.Context, wStatus *grpcwirev1.GWireStatus) error {

	retryErr := retry.RetryOnConflict(retry.DefaultRetry, func() error {
		node, err := gWClient.GetWireObjGrpUS(ctx, wStatus)
		if err != nil {
			grpcOvrlyLogger.Errorf("deleteGRPCWireStatus: failed to read node %s, pod %s@%s from K8s to delete wire status: %v",
				wStatus.LocalNodeName, wStatus.LocalPodName, wStatus.LocalPodIfaceName, err)
			return err
		}

		// TBD: think about a faster way to remove an entry

		newSList := []interface{}{}
		gwireItems, found, err := unstructured.NestedSlice(node.Object, kStatus, kGrpcWireItems)
		if err != nil {
			grpcOvrlyLogger.Errorf("deleteGRPCWireStatus: could not retrieve gWireItems: %v", err)
			return err
		}
		if !found {
			grpcOvrlyLogger.Errorf("deleteGRPCWireStatus: gwireItems not found in GWireKObj status, retrieved from k8s data-store")
			return err
		}
		if gwireItems == nil {
			grpcOvrlyLogger.Errorf("deleteGRPCWireStatus: gwireItems is nil in GWireKObj status, retrieved from k8s data-store")
			return err
		}

		for _, gwireItem := range gwireItems {
			gwireStatusItem, ok := gwireItem.(map[string]interface{})
			if !ok {
				log.Errorf("deleteGRPCWireStatus: unable to retrieve status, %v is not a map", gwireItem)
				continue
			}
			gwireStatus := grpcwirev1.GWireStatus{}
			if err := runtime.DefaultUnstructuredConverter.FromUnstructured(gwireStatusItem, &gwireStatus); err != nil {
				log.Errorf("deleteGRPCWireStatus: unable to convert status from object: %v", err)
				continue
			}
			if gwireStatus.LinkId == wStatus.LinkId {
				continue
			}
			newSList = append(newSList, gwireStatusItem)
		}

		if err := unstructured.SetNestedField(node.Object, newSList, kStatus, kGrpcWireItems); err != nil {
			grpcOvrlyLogger.Errorf("deleteGRPCWireStatus: could not update kGrpcWireItems in status: %v", err)
			return err
		}
		_, err = gWClient.UpdateWireObj(ctx, wStatus.TopoNamespace, node)
		if err == nil {
			grpcOvrlyLogger.Infof("deleteGRPCWireStatus: Deleted GRPCWire status on node %s, for pod %s@%s",
				node.GetName(), wStatus.LocalPodName, wStatus.LocalPodIfaceName)
		}
		return err
	})
	if retryErr != nil {
		log.WithFields(log.Fields{
			"daemon":   "meshnetd",
			"err":      retryErr,
			"function": "deleteGRPCWireStatus",
		}).Errorf("Failed to update status on node %s, pod %s@%s", wStatus.LocalNodeName, wStatus.LocalPodName, wStatus.LocalPodIfaceName)
		return retryErr
	}

	return nil
}

// -----------------------------------------------------------------------------------------------------------
// reCreateGWire writes the wire status 'wStatus' retrieved from k8s data-store into local memory database
// 'in-memory wire-map' and starts pod to daemon packet receive thread for this wire.
func reCreateGWire(wStatus grpcwirev1.GWireStatus, _ context.Context) error {

	grpcWire, ok := GetWireByUID(wStatus.LocalPodNetNs, int(wStatus.LinkId))
	if ok && grpcWire.IsReady {
		grpcOvrlyLogger.Infof("reCreateGWire: This grpc-wire is already present in local db, link id %d", wStatus.LinkId)
		return nil
	}

	wireDef := mpb.WireDef{
		LinkUid:               wStatus.LinkId,
		WireIfNameOnLocalNode: wStatus.WireIfaceNameOnLocalNode,
		LocalPodIp:            wStatus.LocalPodIp,
		IntfNameInPod:         wStatus.LocalPodIfaceName,
		LocalPodName:          wStatus.LocalPodName,
		LocalPodNetNs:         wStatus.LocalPodNetNs,
		WireIfIdOnPeerNode:    wStatus.WireIfaceIdOnPeerNode,
		PeerNodeIp:            wStatus.GWirePeerNodeIp,
		TopoNs:                wStatus.TopoNamespace,
	}
	err := reconLocalGRPCWire(&wireDef)
	if err != nil {
		return fmt.Errorf("reCreateGWire: Failed to reconciliate local end of the GRPC channel: %v", err)
	}

	grpcOvrlyLogger.Infof("Reconciliated grpc-wire (local-pod:%s:%s@node:%s <----link uid: %d----> remote-peer:%s:%d)",
		wStatus.LocalPodName, wStatus.LocalPodIfaceName, wStatus.WireIfaceNameOnLocalNode,
		wStatus.LinkId, wStatus.GWirePeerNodeIp, wStatus.WireIfaceIdOnPeerNode)

	return nil
}

// -----------------------------------------------------------------------------------------------------------
// Recreate the wire in-memory wire-map and start the pod to daemon packet receive thread for this wire.
func reconLocalGRPCWire(wireDef *mpb.WireDef) error {
	locInf, err := net.InterfaceByName(wireDef.WireIfNameOnLocalNode)
	if err != nil {
		grpcOvrlyLogger.Errorf("[RECONCILE:LOCAL-END]For pod %s failed to retrieve interface ID for interface %v. error:%v", wireDef.LocalPodName, wireDef.WireIfNameOnLocalNode, err)
		return err
	}

	//Using google gopacket for packet receive. An alternative could be using socket. Not sure it it provides any advantage over gopacket.
	wrHandle, err := pcap.OpenLive(wireDef.WireIfNameOnLocalNode, 65365, true, pcap.BlockForever)
	if err != nil {
		grpcOvrlyLogger.Errorf("[RECONCILE:LOCAL-END]Could not open interface for send/recv packets for containers local iface id %d. error:%v", locInf.Index, err)
		return err
	}
	aWire := CreateGWire(locInf.Index, wireDef.WireIfNameOnLocalNode, make(chan struct{}), wireDef)
	aWire.IsReady = true
	// reconciling, so add only in memory
	wires.AddInMem(aWire, wrHandle)

	// TODO: handle error here
	go RecvFrmLocalPodThread(aWire, aWire.LocalNodeIfaceName)

	return nil
}

// Finds out the node name in which a pod is running. A running pod can call this function to find
// out the node in which it's currently running. This function must be called from within the cluster.
// Returns the "node name" and error
func findNodeName() (string, error) {
	var err error

	//Ref - Expose Pod Information to Containers Through Environment Variables
	//https://kubernetes.io/docs/tasks/inject-data-application/environment-variable-expose-pod-information/

	// NODE_NAME for meshnet daemon set it carries the "spec.nodeName" for the daemon set.
	ndNm := os.Getenv("NODE_NAME")
	if len(ndNm) == 0 {
		//grpcOvrlyLogger.Infof("Couldn't find node name from environment. Check the daemonset.yaml has NODE_NAME env set to spec.nodeName. Retrieving it from OS.\n")
		ndNm, err = os.Hostname()
		if err != nil {
			return "", fmt.Errorf("findNodeName: could not get node name from OS: %v", err)
		}
	}
	return ndNm, nil
}
