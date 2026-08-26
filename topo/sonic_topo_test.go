package topo_test

import (
	"context"
	"testing"
	"time"

	tpb "github.com/openconfig/kne/proto/topo"
	topologyv1 "github.com/openconfig/kne/third_party/meshnet/api/types/v1beta1"
	"github.com/openconfig/kne/topo"
	_ "github.com/openconfig/kne/topo/node/sonic"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	dfake "k8s.io/client-go/dynamic/fake"
	kfake "k8s.io/client-go/kubernetes/fake"
	rest "k8s.io/client-go/rest"
	ktest "k8s.io/client-go/testing"
)

func TestCreateSonicNode(t *testing.T) {
	ctx := context.Background()

	tf := dfake.NewSimpleDynamicClient(topologyv1.Scheme)

	kf := kfake.NewSimpleClientset()
	kf.PrependReactor("get", "pods", func(action ktest.Action) (bool, runtime.Object, error) {
		gAction, ok := action.(ktest.GetAction)
		if !ok {
			return false, nil, nil
		}
		p := &corev1.Pod{
			ObjectMeta: metav1.ObjectMeta{Name: gAction.GetName()},
			Status: corev1.PodStatus{
				Phase:      corev1.PodRunning,
				Conditions: []corev1.PodCondition{{Type: corev1.PodReady, Status: corev1.ConditionTrue}},
			},
		}
		return true, p, nil
	})

	spec := &tpb.Topology{
		Name: "test-sonic",
		Nodes: []*tpb.Node{
			{
				Name:   "s1",
				Vendor: tpb.Vendor_SONIC,
			},
		},
	}

	opts := []topo.Option{
		topo.WithClusterConfig(&rest.Config{}),
		topo.WithKubeClient(kf),
		topo.WithDynamicClient(tf),
	}

	m, err := topo.New(spec, opts...)
	if err != nil {
		t.Fatalf("New() failed: %v", err)
	}

	if err := m.Create(ctx, 1*time.Second); err != nil {
		t.Fatalf("Create() failed: %v", err)
	}
}
