package fake

import (
	metallbv1client "github.com/openconfig/kne/api/metallb/clientset/v1beta1"
	"k8s.io/apimachinery/pkg/runtime"
	dfake "k8s.io/client-go/dynamic/fake"
	"k8s.io/client-go/rest"
)

func NewSimpleClientset(objects ...runtime.Object) (*metallbv1client.Clientset, error) {
	cs, err := metallbv1client.NewAddressPoolForConfig(&rest.Config{})
	if err != nil {
		return nil, err
	}
	c := dfake.NewSimpleDynamicClient(metallbv1client.Scheme, objects...)
	cs.SetDynamicClient(c.Resource(metallbv1client.IPAddressPoolGVR()))
	return cs, nil
}
