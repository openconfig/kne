package grpcwire

import (
	"context"
	"errors"
	"fmt"
	"os"
	"reflect"
	"sync"

	grpcwirev1 "github.com/openconfig/kne/third_party/meshnet/api/types/v1beta1"
	mpb "github.com/openconfig/kne/third_party/meshnet/daemon/proto/meshnet/v1beta1"
	"github.com/openconfig/kne/third_party/meshnet/utils/wireutil"
	log "github.com/sirupsen/logrus"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
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

// SetGWireClient initializes the dynamic K8s client for gRPC wire CRD management.
func SetGWireClient(gClient *dynamic.DynamicClient) {
	// identifier<group, version, resource> of grpc wire object in k8s apis
	gWClient.gvr = schema.GroupVersionResource{
		Group:    grpcwirev1.GroupName,
		Version:  grpcwirev1.GroupVersion,
		Resource: grpcwirev1.GWireResNamePlural,
	}
	gWClient.di = gClient.Resource(gWClient.gvr)
}

// SetGWireClientInterface sets the K8s dynamic resource interface (used for unit testing).
func SetGWireClientInterface(gClient dynamic.NamespaceableResourceInterface) {
	gWClient.di = gClient
}

// GetWireObjListUS lists unstructured GWireKObj resources for a specified node.
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

// CreatWireObj creates a new unstructured GWireKObj resource in K8s.
func (gc GWireClient) CreatWireObj(ctx context.Context, nSpace string, uWbj map[string]interface{}) (*unstructured.Unstructured, error) {
	return gc.di.Namespace(nSpace).Create(ctx, &unstructured.Unstructured{Object: uWbj}, metav1.CreateOptions{})
}

// UpdateWireObj updates an existing unstructured GWireKObj resource in K8s.
func (gc GWireClient) UpdateWireObj(ctx context.Context, nSpace string, wObjsOnNd *unstructured.Unstructured) (*unstructured.Unstructured, error) {
	return gc.di.Namespace(nSpace).Update(ctx, wObjsOnNd, metav1.UpdateOptions{})
}

// GetWireObjGrpUS retrieves the GWireKObj for a given node and status.
func (gc GWireClient) GetWireObjGrpUS(ctx context.Context, wStatus *grpcwirev1.GWireStatus) (*unstructured.Unstructured, error) {
	return gc.di.Namespace(wStatus.TopoNamespace).Get(ctx, wStatus.LocalNodeName, metav1.GetOptions{})
}

// -----------------------------------------------------------------------------------------------------------
// Create & populate "GWireStatus" from a "GRPCWire". GWireStatus is stored in K8S data-store
func CreateWireStatus(wire *GRPCWire, nodeName string) *grpcwirev1.GWireStatus {
	wire.mu.Lock()
	defer wire.mu.Unlock()

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

type wireStatusUpdate struct {
	wire      *GRPCWire
	nodeName  string
	flushDone chan struct{}
}

var (
	statusQueueChan = make(chan wireStatusUpdate, 10000)
	statusQueueOnce sync.Once
)

func startStatusQueueWorker() {
	statusQueueOnce.Do(func() {
		go func() {
			for {
				first, ok := <-statusQueueChan
				if !ok {
					return
				}

				var flushSignals []chan struct{}
				var updates []wireStatusUpdate

				if first.flushDone != nil {
					flushSignals = append(flushSignals, first.flushDone)
				} else {
					updates = append(updates, first)
				}

				drain := true
				for drain && len(updates) < 200 {
					select {
					case u, ok := <-statusQueueChan:
						if !ok {
							drain = false
						} else if u.flushDone != nil {
							flushSignals = append(flushSignals, u.flushDone)
							drain = false
						} else {
							updates = append(updates, u)
						}
					default:
						drain = false
					}
				}

				if len(updates) > 0 {
					if err := updateGRPCWireStatusBatch(context.Background(), updates); err != nil {
						grpcOvrlyLogger.Warnf("K8sStatusWorker: failed to update batch of %d wire statuses: %v", len(updates), err)
					}
				}

				for _, sig := range flushSignals {
					close(sig)
				}
			}
		}()
	})
}

// FlushK8sStatusQueue blocks until all currently queued status updates have been processed by the worker.
func FlushK8sStatusQueue() {
	startStatusQueueWorker()
	done := make(chan struct{})
	statusQueueChan <- wireStatusUpdate{flushDone: done}
	<-done
}

type statusGroupKey struct {
	nodeName string
	topoNs   string
}

func updateGRPCWireStatusBatch(ctx context.Context, updates []wireStatusUpdate) error {
	if len(updates) == 0 {
		return nil
	}

	var activeUpdates []wireStatusUpdate
	for _, u := range updates {
		if _, ok := GetWireByUID(u.wire.LocalPodNetNS, u.wire.UID); ok {
			activeUpdates = append(activeUpdates, u)
		}
	}
	if len(activeUpdates) == 0 {
		return nil
	}

	groups := make(map[statusGroupKey][]wireStatusUpdate)
	for _, u := range activeUpdates {
		k := statusGroupKey{
			nodeName: u.nodeName,
			topoNs:   u.wire.TopoNamespace,
		}
		groups[k] = append(groups[k], u)
	}

	var errs []error
	for key, grpUpdates := range groups {
		if err := updateGRPCWireStatusGroup(ctx, key.nodeName, key.topoNs, grpUpdates); err != nil {
			errs = append(errs, err)
		}
	}
	return errors.Join(errs...)
}

func updateGRPCWireStatusGroup(ctx context.Context, nodeName, topoNs string, updates []wireStatusUpdate) error {
	if len(updates) == 0 {
		return nil
	}

	return retry.RetryOnConflict(retry.DefaultRetry, func() error {
		wStatusFirst := CreateWireStatus(updates[0].wire, nodeName)
		wObjsOnNd, err := gWClient.GetWireObjGrpUS(ctx, wStatusFirst)
		if err != nil {
			if apierrors.IsNotFound(err) {
				err = CreateGWireStatInDS(ctx, wStatusFirst)
				if err != nil {
					return err
				}
				wObjsOnNd, err = gWClient.GetWireObjGrpUS(ctx, wStatusFirst)
				if err != nil {
					return err
				}
			} else {
				return err
			}
		}

		gwireItems, found, err := unstructured.NestedSlice(wObjsOnNd.Object, kStatus, kGrpcWireItems)
		if err != nil || !found || gwireItems == nil {
			gwireItems = []interface{}{}
		}

		existingMap := make(map[string]int)
		for i, item := range gwireItems {
			if m, ok := item.(map[string]interface{}); ok {
				key := fmt.Sprintf("%v@%v", m["local_pod_name"], m["local_pod_iface_name"])
				existingMap[key] = i
			}
		}

		for _, u := range updates {
			if _, ok := GetWireByUID(u.wire.LocalPodNetNS, u.wire.UID); !ok {
				continue
			}

			ws := CreateWireStatus(u.wire, u.nodeName)
			unstrucObj, err := runtime.DefaultUnstructuredConverter.ToUnstructured(ws)
			if err != nil {
				continue
			}

			key := fmt.Sprintf("%v@%v", ws.LocalPodName, ws.LocalPodIfaceName)
			if idx, exists := existingMap[key]; exists {
				gwireItems[idx] = unstrucObj
			} else {
				gwireItems = append(gwireItems, unstrucObj)
				existingMap[key] = len(gwireItems) - 1
			}
		}

		if err := unstructured.SetNestedField(wObjsOnNd.Object, gwireItems, kStatus, kGrpcWireItems); err != nil {
			return err
		}

		_, err = gWClient.UpdateWireObj(ctx, topoNs, wObjsOnNd)
		return err
	})
}

// -----------------------------------------------------------------------------------------------------------
// K8sStoreGWire enqueues grpc wire info 'wire' for a specific topology namespace (wire.TopoNamespace)
// into the background status queue to be written in batch to the k8s data-store for the current node.
func (wire *GRPCWire) K8sStoreGWire() error {
	startStatusQueueWorker()
	nodeName, err := findNodeName()
	if err != nil {
		grpcOvrlyLogger.Errorf("K8sStoreGWire: could not get node name: %v", err)
		return err
	}

	select {
	case statusQueueChan <- wireStatusUpdate{wire: wire, nodeName: nodeName}:
	default:
		grpcOvrlyLogger.Warnf("K8sStoreGWire: status queue full, dropping async status update for %s@%s", wire.LocalPodName, wire.LocalPodIfaceName)
	}
	return nil
}

// -----------------------------------------------------------------------------------------------------------
// K8sDelGWire deletes grpc wire info 'wire' for a specific namespace from k8s api data-store for the current
// node. namespace is specified in given 'wire' argument. it calls deleteGRPCWireStatus() to serve the purpose
func (wire *GRPCWire) K8sDelGWire() error {
	// Flush any pending async status updates before performing deletion
	FlushK8sStatusQueue()

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
			if apierrors.IsNotFound(err) {
				return nil
			}
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
	tapFile, err := wireutil.CreateOrAttachTAP(wireDef.LocalPodNetNs, wireDef.IntfNameInPod, wireDef.LocalPodIp)
	if err != nil {
		grpcOvrlyLogger.Errorf("[RECONCILE:LOCAL-END] For pod %s failed to create/attach TAP interface %s in netns %s: %v",
			wireDef.LocalPodName, wireDef.IntfNameInPod, wireDef.LocalPodNetNs, err)
		return err
	}
	wireID := NextIndex()
	aWire := CreateGWire(int(wireID), wireDef.IntfNameInPod, make(chan struct{}), wireDef)
	aWire.IsReady = true
	// reconciling, so add only in memory
	wires.AddInMem(aWire, tapFile)

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
