package fake

import (
	toplogyv1client "github.com/openconfig/kne/api/clientset/v1beta1"
	topologyv1 "github.com/openconfig/kne/api/types/v1beta1"

	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	dfake "k8s.io/client-go/dynamic/fake"
	"k8s.io/client-go/rest"
)

func NewSimpleClientset(objects ...runtime.Object) (*toplogyv1client.Clientset, error) {
	cs, err := toplogyv1client.NewForConfig(&rest.Config{})
	if err != nil {
		return nil, err
	}
	c := dfake.NewSimpleDynamicClientWithCustomListKinds(topologyv1.Scheme, map[schema.GroupVersionResource]string{
		toplogyv1client.GVR(): "TopologyList",
	}, objects...)
	cs.SetDynamicClient(c.Resource(toplogyv1client.GVR()))
	return cs, nil
}
