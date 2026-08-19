package grpcwire

import (
	"context"
	"net"
	"os"
	"reflect"
	"sync"
	"testing"

	"github.com/containernetworking/plugins/pkg/ns"
	"github.com/google/go-cmp/cmp"
	grpcwirev1 "github.com/openconfig/kne/third_party/meshnet/api/types/v1beta1"
	"github.com/vishvananda/netlink"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/fields"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/dynamic"
	dynamicfake "k8s.io/client-go/dynamic/fake"
)

var (
	gWireKObj1 = &grpcwirev1.GWireKObj{
		TypeMeta: metav1.TypeMeta{
			Kind:       reflect.TypeOf(grpcwirev1.GWireKObj{}).Name(),
			APIVersion: grpcwirev1.GroupName + "/" + grpcwirev1.GroupVersion,
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      must(findNodeName()),
			Namespace: "test",
		},
		Status: grpcwirev1.GWireKNodeStatus{
			GWireKItems: []grpcwirev1.GWireStatus{
				{
					LocalNodeName:            must(findNodeName()),
					LinkId:                   1,
					TopoNamespace:            "test",
					LocalPodNetNs:            "testNetNs",
					LocalPodName:             "pod1",
					LocalPodIp:               "1.1.1.1",
					LocalPodIfaceName:        "eth1",
					WireIfaceNameOnLocalNode: "eth1-node1",
					WireIfaceIdOnPeerNode:    101,
					GWirePeerNodeIp:          "2.2.2.2",
				},
				{
					LocalNodeName:            must(findNodeName()),
					LinkId:                   2,
					TopoNamespace:            "test",
					LocalPodNetNs:            "testNetNs",
					LocalPodName:             "pod2",
					LocalPodIp:               "1.1.1.2",
					LocalPodIfaceName:        "eth2",
					WireIfaceNameOnLocalNode: "eth2-node1",
					WireIfaceIdOnPeerNode:    102,
					GWirePeerNodeIp:          "2.2.2.2",
				},
			},
		},
		Spec: grpcwirev1.GWireKNodeSpec{},
	}
	gWireKObj2 = &grpcwirev1.GWireKObj{
		TypeMeta: metav1.TypeMeta{
			Kind:       reflect.TypeOf(grpcwirev1.GWireKObj{}).Name(),
			APIVersion: grpcwirev1.GroupName + "/" + grpcwirev1.GroupVersion,
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      must(findNodeName()),
			Namespace: "test2",
		},
		Status: grpcwirev1.GWireKNodeStatus{
			GWireKItems: []grpcwirev1.GWireStatus{
				{
					LocalNodeName:            must(findNodeName()),
					LinkId:                   3,
					TopoNamespace:            "test2",
					LocalPodNetNs:            "testNetNs",
					LocalPodName:             "pod3",
					LocalPodIp:               "2.1.1.1",
					LocalPodIfaceName:        "eth3",
					WireIfaceNameOnLocalNode: "eth3-node2",
					WireIfaceIdOnPeerNode:    201,
					GWirePeerNodeIp:          "2.2.2.2",
				},
			},
		},
		Spec: grpcwirev1.GWireKNodeSpec{},
	}
)

func must[T any](v T, err error) T {
	if err != nil {
		panic(err)
	}
	return v
}

func setUp(t *testing.T, objs ...runtime.Object) dynamic.NamespaceableResourceInterface {
	//t.Helper()
	InitLogger()
	//objs := []runtime.Object{}

	var gvr schema.GroupVersionResource = schema.GroupVersionResource{
		Group:    grpcwirev1.GroupName,
		Version:  grpcwirev1.GroupVersion,
		Resource: grpcwirev1.GWireResNamePlural,
	}
	f := dynamicfake.NewSimpleDynamicClient(grpcwirev1.Scheme, objs...)
	resInterface := f.Resource(gvr)
	SetGWireClientInterface(resInterface)

	return resInterface
}

func createWireFromWireStatus(wireStatus *grpcwirev1.GWireStatus) *GRPCWire {
	return &GRPCWire{
		// LocalNodeName: nodeName,
		UID:           int(wireStatus.LinkId),
		TopoNamespace: wireStatus.TopoNamespace,

		// local pod information
		LocalPodNetNS:      wireStatus.LocalPodNetNs,
		LocalNodeIfaceName: wireStatus.WireIfaceNameOnLocalNode,
		LocalPodName:       wireStatus.LocalPodName,
		LocalPodIfaceName:  wireStatus.LocalPodIfaceName,
		LocalPodIP:         wireStatus.LocalPodIp,

		// peer information
		WireIfaceIDOnPeerNode: wireStatus.WireIfaceIdOnPeerNode,
		PeerNodeIP:            wireStatus.GWirePeerNodeIp,
	}
}

func getWireObjListUSForNs(cs dynamic.NamespaceableResourceInterface, ndName, ns string) (*unstructured.UnstructuredList, error) {
	return cs.Namespace(ns).List(context.Background(), metav1.ListOptions{
		TypeMeta: metav1.TypeMeta{
			Kind: reflect.TypeOf(grpcwirev1.GWireKObj{}).Name(),
		},
		FieldSelector: fields.SelectorFromSet(
			fields.Set{metav1.ObjectNameField: ndName},
		).String(),
	})
}

// --------------------------------------------------------------------------------------------------
func isRoot(t *testing.T) {
	if os.Getuid() != 0 {
		t.Skip("Test requires root privileges. Run test with sudo")
	}
}

// Create a veth pair in the current name space. It also makes the links up
func createVethPair(t *testing.T, name string, peerName string) error {
	veth := &netlink.Veth{
		LinkAttrs: netlink.LinkAttrs{
			Name:  name,
			Flags: net.FlagUp,
		},
		PeerName: peerName,
	}
	err := netlink.LinkAdd(veth)
	if err != nil {
		switch {
		case os.IsExist(err):
			t.Logf("veth name (%v) already exists", name)
		default:
			t.Logf("netlink failed to make veth pair: %v", err)
		}
		return err
	}
	link, err := netlink.LinkByName(name)
	if err != nil {
		t.Logf("failed to get interface %s, err:%v", name, err)
		return err
	}

	if err = netlink.LinkSetUp(link); err != nil {
		t.Logf("failed to set interface %s up: %v", name, err)
		return err
	}

	link, err = netlink.LinkByName(peerName)
	if err != nil {
		t.Logf("failed to get interface %s, err:%v", peerName, err)
		return err
	}

	if err = netlink.LinkSetUp(link); err != nil {
		t.Logf("failed to set interface %s up: %v", peerName, err)
		return err
	}
	return nil
}

// --------------------------------------------------------------------------------------------------
func cleanupVethPair(t *testing.T, netNs ns.NetNS, ifaceName string) error {
	var err error

	isRoot(t)

	err = netNs.Do(func(_ ns.NetNS) error {
		// deleting only one will delete the pair.
		link2, err := netlink.LinkByName(ifaceName)
		if err != nil {
			t.Errorf("failed to lookup %q in %q: %v", ifaceName, netNs.Path(), err)
			return err
		}
		if err = netlink.LinkDel(link2); err != nil {
			t.Errorf("failed to remove link %q in %q: %v", ifaceName, netNs.Path(), err)
			return err
		}
		return nil
	})

	if err != nil {
		t.Errorf("cleanup: failed to remove link : %v", err)
		return err
	}
	return nil
}

// TestK8sStoreGWire covers gwire status add, update and get commands
func TestK8sStoreGWire(t *testing.T) {
	cs := setUp(t)

	test_cases := []struct {
		desc    string
		store   *GRPCWire
		want    *GRPCWire
		wantErr string
	}{
		{
			desc: "Create",
			store: &GRPCWire{
				UID:                   1,
				TopoNamespace:         "testNs1",
				LocalPodNetNS:         "gwireNetNs",
				LocalNodeIfaceName:    "localNodeIfaceName",
				LocalPodName:          "pod1",
				LocalPodIfaceName:     "localPodIfaceName",
				LocalPodIP:            "10.10.10.1",
				WireIfaceIDOnPeerNode: 100,
				PeerNodeIP:            "100.100.100.100",
			},
		},
		{
			desc: "Update",
			store: &GRPCWire{
				UID:                   2,
				TopoNamespace:         "testNs1",
				LocalPodNetNS:         "gwireNetNs",
				LocalNodeIfaceName:    "localNodeIfaceName1",
				LocalPodName:          "pod2",
				LocalPodIfaceName:     "localPodIfaceName1",
				LocalPodIP:            "10.10.10.2",
				WireIfaceIDOnPeerNode: 101,
				PeerNodeIP:            "100.100.100.101",
			},
		},
	}

	nodeName, err := findNodeName()
	if err != nil {
		t.Fatalf("could not retrieve node name")
	}
	//t.Logf("----------node name %s\n", nodeName)
	var storedWStatus []interface{}
	for _, tc := range test_cases {
		t.Run(tc.desc, func(t *testing.T) {
			_ = wires.AddInMem(tc.store, nil)
			err := tc.store.K8sStoreGWire()
			if err != nil {
				t.Fatalf("could not add gwire status into k8s data-store")
			}
			FlushK8sStatusQueue()
			storedWStatus = append(storedWStatus, *CreateWireStatus(tc.store, nodeName))
			wObjsOnNd, err := cs.Namespace(tc.store.TopoNamespace).Get(context.Background(), nodeName, metav1.GetOptions{})
			if err != nil {
				t.Fatalf("error in retrieving wire obj from k8s data-store: %v", err)
			}
			var retrievedWStatus []interface{}
			grpcWireItems, _, _ := unstructured.NestedSlice(wObjsOnNd.Object, kStatus, kGrpcWireItems)
			for _, gWireItem := range grpcWireItems {
				var wireStatus = grpcwirev1.GWireStatus{}
				err = runtime.DefaultUnstructuredConverter.FromUnstructured(gWireItem.(map[string]interface{}), &wireStatus)
				if err != nil {
					t.Fatalf("could not convert to wire status")
				}
				retrievedWStatus = append(retrievedWStatus, wireStatus)
			}
			if !cmp.Equal(storedWStatus, retrievedWStatus) {
				t.Errorf("could not retrieve correct gwire info")
				if s := cmp.Diff(storedWStatus, retrievedWStatus); s != "" {
					t.Logf("Retrieved info:\n(%+v)\n", retrievedWStatus)
					t.Logf("Stored info:\n(%+v)\n", storedWStatus)
					t.Logf("Diff info:\n(%+v)\n", s)
				}
			}
		})
	}
	//t.Logf("TestK8sStoreGWire: passed")
}

func TestK8sStoreGWire_MultiNamespaceBatch(t *testing.T) {
	cs := setUp(t)
	nodeName, err := findNodeName()
	if err != nil {
		t.Fatalf("could not retrieve node name: %v", err)
	}

	wireNs1 := &GRPCWire{
		UID:                   1,
		TopoNamespace:         "topo-ns1",
		LocalPodNetNS:         "netns1",
		LocalNodeIfaceName:    "eth1-node1",
		LocalPodName:          "pod1",
		LocalPodIfaceName:     "eth1",
		LocalPodIP:            "10.1.1.1",
		WireIfaceIDOnPeerNode: 101,
		PeerNodeIP:            "192.168.1.2",
	}
	wireNs2 := &GRPCWire{
		UID:                   2,
		TopoNamespace:         "topo-ns2",
		LocalPodNetNS:         "netns2",
		LocalNodeIfaceName:    "eth1-node2",
		LocalPodName:          "pod2",
		LocalPodIfaceName:     "eth1",
		LocalPodIP:            "10.2.2.2",
		WireIfaceIDOnPeerNode: 201,
		PeerNodeIP:            "192.168.1.3",
	}

	_ = wires.AddInMem(wireNs1, nil)
	_ = wires.AddInMem(wireNs2, nil)

	// Enqueue both updates to be batched together
	if err := wireNs1.K8sStoreGWire(); err != nil {
		t.Fatalf("failed to store wireNs1: %v", err)
	}
	if err := wireNs2.K8sStoreGWire(); err != nil {
		t.Fatalf("failed to store wireNs2: %v", err)
	}

	FlushK8sStatusQueue()

	// Verify topo-ns1 has wireNs1
	wObjs1, err := cs.Namespace("topo-ns1").Get(context.Background(), nodeName, metav1.GetOptions{})
	if err != nil {
		t.Fatalf("failed to get topo-ns1 object: %v", err)
	}
	items1, _, _ := unstructured.NestedSlice(wObjs1.Object, kStatus, kGrpcWireItems)
	if len(items1) != 1 {
		t.Fatalf("expected 1 item in topo-ns1, got %d", len(items1))
	}
	var ws1 grpcwirev1.GWireStatus
	_ = runtime.DefaultUnstructuredConverter.FromUnstructured(items1[0].(map[string]interface{}), &ws1)
	if ws1.TopoNamespace != "topo-ns1" || ws1.LocalPodName != "pod1" {
		t.Errorf("unexpected status in topo-ns1: %+v", ws1)
	}

	// Verify topo-ns2 has wireNs2
	wObjs2, err := cs.Namespace("topo-ns2").Get(context.Background(), nodeName, metav1.GetOptions{})
	if err != nil {
		t.Fatalf("failed to get topo-ns2 object: %v", err)
	}
	items2, _, _ := unstructured.NestedSlice(wObjs2.Object, kStatus, kGrpcWireItems)
	if len(items2) != 1 {
		t.Fatalf("expected 1 item in topo-ns2, got %d", len(items2))
	}
	var ws2 grpcwirev1.GWireStatus
	_ = runtime.DefaultUnstructuredConverter.FromUnstructured(items2[0].(map[string]interface{}), &ws2)
	if ws2.TopoNamespace != "topo-ns2" || ws2.LocalPodName != "pod2" {
		t.Errorf("unexpected status in topo-ns2: %+v", ws2)
	}
}

func TestFlushK8sStatusQueue_Concurrent(t *testing.T) {
	_ = setUp(t)
	var wg sync.WaitGroup
	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			wire := &GRPCWire{
				UID:                   1000 + id,
				TopoNamespace:         "topo-flush",
				LocalPodNetNS:         "netns-flush",
				LocalNodeIfaceName:    "eth1-flush",
				LocalPodName:          "pod-flush",
				LocalPodIfaceName:     "eth1",
				LocalPodIP:            "10.0.0.1",
				WireIfaceIDOnPeerNode: int64(100 + id),
				PeerNodeIP:            "192.168.1.10",
			}
			_ = wires.AddInMem(wire, nil)
			_ = wire.K8sStoreGWire()
			FlushK8sStatusQueue()
		}(i)
	}
	wg.Wait()
}

func TestK8sStoreGWire_CreateThenDeleteGhostWirePrevention(t *testing.T) {
	cs := setUp(t)
	nodeName, err := findNodeName()
	if err != nil {
		t.Fatalf("could not retrieve node name: %v", err)
	}

	wire := &GRPCWire{
		UID:                   999,
		TopoNamespace:         "topo-ghost",
		LocalPodNetNS:         "netns-ghost",
		LocalNodeIfaceName:    "eth1-ghost",
		LocalPodName:          "pod-ghost",
		LocalPodIfaceName:     "eth1",
		LocalPodIP:            "10.9.9.9",
		WireIfaceIDOnPeerNode: 999,
		PeerNodeIP:            "192.168.9.9",
	}

	// 1. Add to in-memory and enqueue async creation
	_ = wires.AddInMem(wire, nil)
	if err := wire.K8sStoreGWire(); err != nil {
		t.Fatalf("failed to store wire: %v", err)
	}

	// 2. Immediately delete wire from memory and call K8sDelGWire
	_ = wires.AtomicDelete(wire)
	if err := wire.K8sDelGWire(); err != nil {
		t.Fatalf("failed to delete wire: %v", err)
	}

	// 3. Flush any residual queue items
	FlushK8sStatusQueue()

	// 4. Verify no ghost wire exists in K8s
	wObjs, err := cs.Namespace("topo-ghost").Get(context.Background(), nodeName, metav1.GetOptions{})
	if err == nil {
		items, _, _ := unstructured.NestedSlice(wObjs.Object, kStatus, kGrpcWireItems)
		if len(items) != 0 {
			t.Fatalf("expected 0 items after deletion, got %d ghost items: %+v", len(items), items)
		}
	}
}

func TestCreateWireStatus_ConcurrentUpdateRace(t *testing.T) {
	wire := &GRPCWire{
		UID:                   555,
		TopoNamespace:         "topo-race",
		LocalPodNetNS:         "netns-race",
		LocalNodeIfaceName:    "eth1-race",
		LocalPodName:          "pod-race",
		LocalPodIfaceName:     "eth1",
		LocalPodIP:            "10.5.5.5",
		WireIfaceIDOnPeerNode: 0,
		PeerNodeIP:            "192.168.5.5",
	}

	var wg sync.WaitGroup
	for i := 0; i < 50; i++ {
		wg.Add(2)
		go func(id int) {
			defer wg.Done()
			wire.UpdateWire(int64(id), nil)
		}(i)
		go func() {
			defer wg.Done()
			_ = CreateWireStatus(wire, "node1")
		}()
	}
	wg.Wait()
}

// TestK8sDelGWire covers gwire status delete, update and get commands
func TestK8sDelGWire(t *testing.T) {
	//objs := []runtime.Object{gWireKObj1, gWireKObj2}
	cs := setUp(t, gWireKObj1, gWireKObj2)

	test_cases := []struct {
		desc    string
		stored  *grpcwirev1.GWireKObj
		exptCnt int
	}{
		{
			desc:    "Multiple wires",
			stored:  gWireKObj1,
			exptCnt: 2,
		},
		{
			desc:    "Single wire",
			stored:  gWireKObj2,
			exptCnt: 1,
		},
	}

	for _, tc := range test_cases {
		t.Run(tc.desc, func(t *testing.T) {
			usObjList, err := getWireObjListUSForNs(cs, tc.stored.GetName(), tc.stored.GetNamespace())
			if err != nil || len(usObjList.Items) != 1 {
				t.Fatalf("could not retrieve gwire status from k8s data-store")
			}
			// usObjList.Items[0] returns gwireKObj. there is only one kobj for this NS and name
			grpcWireItems, _, _ := unstructured.NestedSlice(usObjList.Items[0].Object, kStatus, kGrpcWireItems)
			if len(grpcWireItems) != tc.exptCnt {
				t.Logf("stored wire status count (%d) is not matching with expected count (%d)",
					len(grpcWireItems), tc.exptCnt)
				t.FailNow()
			}
			for _, gWireStatus := range tc.stored.Status.GWireKItems {
				gWire := createWireFromWireStatus(&gWireStatus)
				err := gWire.K8sDelGWire()
				if err != nil {
					t.Fatalf("could not delete gwire status from k8s data-store")
				}
			}
			usObjList2, err := getWireObjListUSForNs(cs, tc.stored.GetName(), tc.stored.GetNamespace())
			if err != nil || len(usObjList2.Items) != 1 {
				t.Fatalf("could not retrieve gwire status from k8s data-store after delete")
			}
			grpcWireItems2, _, _ := unstructured.NestedSlice(usObjList2.Items[0].Object, kStatus, kGrpcWireItems)
			if len(grpcWireItems2) != 0 {
				t.Logf("stored wire status count (%d) is not matching with expected count (0) after delete",
					len(usObjList2.Items))
				t.FailNow()
			}
		})
	}
}

// TestReconGWires covers gwire reconciliation into local memory
func TestReconGWires(t *testing.T) {
	cs := setUp(t, gWireKObj1, gWireKObj2)

	test_cases := []struct {
		desc    string
		stored  *grpcwirev1.GWireKObj
		exptCnt int
	}{
		{
			desc:   "Multiple wires",
			stored: gWireKObj1,
		},
		{
			desc:   "Single wire",
			stored: gWireKObj2,
		},
	}

	isRoot(t)

	// create interface first
	for _, tc := range test_cases {
		for _, gWireStatus := range tc.stored.Status.GWireKItems {
			err := createVethPair(t, gWireStatus.LocalPodIfaceName, gWireStatus.WireIfaceNameOnLocalNode)
			if err != nil {
				t.Logf("createVethPair returned error: %v", err)
				t.FailNow()
			}
		}
	}
	//clean up
	currNs, err := ns.GetCurrentNS()
	if err != nil {
		t.Logf("failed to get current namespace : %v", err)
		t.FailNow()
	}
	defer currNs.Close()

	// cleanup interfaces
	defer func() {
		for _, tc := range test_cases {
			for _, gWireStatus := range tc.stored.Status.GWireKItems {
				err := cleanupVethPair(t, currNs, gWireStatus.LocalPodIfaceName)
				if err != nil {
					t.Logf("cleanup: failed to remove link : %v", err)
					t.FailNow()
				}
			}
		}
	}()

	err = ReconGWires()
	if err != nil {
		t.Logf("reconciliation fails: %v", err)
		t.FailNow()
	}

	//verify local memory database
	for _, tc := range test_cases {
		t.Run(tc.desc, func(t *testing.T) {
			usObjList, err := getWireObjListUSForNs(cs, tc.stored.GetName(), tc.stored.GetNamespace())
			if err != nil {
				t.Fatalf("could not retrieve gwire status from k8s data-store")
			}
			// usObjList.Items[0] returns gwireKObj. there is only one kobj for this NS and name
			grpcWireItems, found, err := unstructured.NestedSlice(usObjList.Items[0].Object, kStatus, kGrpcWireItems)
			if err != nil || !found {
				t.Logf("GWire status item not found in k8s data store")
				t.FailNow()
			}
			for _, gWireStatus := range grpcWireItems {
				var wireStatus = grpcwirev1.GWireStatus{}
				err = runtime.DefaultUnstructuredConverter.FromUnstructured(gWireStatus.(map[string]interface{}), &wireStatus)
				if err != nil {
					t.Fatalf("could not convert to wire status")
				}
				gWireLocal, found := wires.GetWire(wireStatus.LocalPodNetNs, int(wireStatus.LinkId))
				if !found {
					t.Logf("could not write into local memory from k8s data-store")
					t.FailNow()
				}
				gWireStatusLocal := CreateWireStatus(gWireLocal, tc.stored.GetName())
				if !cmp.Equal(wireStatus, *gWireStatusLocal) {
					t.Errorf("could not retrieve correct gwire status")
					if s := cmp.Diff(wireStatus, gWireStatusLocal); s != "" {
						//t.Logf("Data-store info:\n(%+v)\n", wireStatus)
						//t.Logf("Local memory info:\n(%+v)\n", gWireStatusLocal)
						t.Logf("Diff info:\n(%+v)\n", s)
						t.FailNow()
					}
				}
			}
		})
	}
}
