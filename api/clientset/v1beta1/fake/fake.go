package fake

import (
	toplogyv1client "github.com/openconfig/kne/api/clientset/v1beta1"
	"k8s.io/apimachinery/pkg/runtime"
	dfake "k8s.io/client-go/dynamic/fake"
	"k8s.io/client-go/kubernetes/scheme"
	"k8s.io/client-go/rest"
)

func NewSimpleClientset(objects ...runtime.Object) (*toplogyv1client.Clientset, error) {
	cs, err := toplogyv1client.NewForConfig(&rest.Config{})
	if err != nil {
		return nil, err
	}
	cs.SetDynamicClient(dfake.NewSimpleDynamicClient(scheme.Scheme, objects...).Resource(toplogyv1client.GVR()))
	return cs, nil
}
