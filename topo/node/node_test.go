package node

import (
	"context"
	"testing"

	"github.com/google/go-cmp/cmp"
	"github.com/google/go-cmp/cmp/cmpopts"
	"github.com/h-fam/errdiff"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/intstr"
	kfake "k8s.io/client-go/kubernetes/fake"
	"k8s.io/client-go/rest"

	topopb "github.com/openconfig/kne/proto/topo"
)

func NewNR(impl *Impl) (Node, error) {
	return &notResettable{Impl: impl}, nil
}

type notResettable struct {
	*Impl
}

type resettable struct {
	*notResettable
}

func (r *resettable) ResetCfg(ctx context.Context) error {
	return nil
}

func NewR(impl *Impl) (Node, error) {
	return &resettable{&notResettable{Impl: impl}}, nil
}

func TestReset(t *testing.T) {
	Register(topopb.Node_Type(1001), NewR)
	Register(topopb.Node_Type(1002), NewNR)
	n, err := New("test", &topopb.Node{Type: topopb.Node_Type(1001)}, nil, nil, "", "")
	if err != nil {
		t.Fatalf("failed to create node: %v", err)
	}
	r, ok := n.(Resetter)
	if !ok {
		t.Fatalf("Resettable node failed to type assert to resetter")
	}
	if err := r.ResetCfg(context.Background()); err != nil {
		t.Errorf("Resettable node failed to reset: %v", err)
	}
	nr, err := New("test", &topopb.Node{Type: topopb.Node_Type(1002)}, nil, nil, "", "")
	if err != nil {
		t.Fatalf("failed to create node: %v", err)
	}
	_, ok = nr.(Resetter)
	if ok {
		t.Errorf("Not-Resettable node type asserted to resetter")
	}
}

func TestService(t *testing.T) {
	tests := []struct {
		desc           string
		node           *topopb.Node
		kClient        *kfake.Clientset
		wantCreateErr  string
		wantServiceErr string
		want           []*corev1.Service
	}{{
		desc:           "no services",
		node:           &topopb.Node{Name: "dev1", Type: topopb.Node_Type(1001)},
		kClient:        kfake.NewSimpleClientset(),
		wantServiceErr: `"service-dev1" not found`,
	}, {
		desc: "services valid",
		node: &topopb.Node{
			Name: "dev1",
			Type: topopb.Node_Type(1001),
			Services: map[uint32]*topopb.Service{
				22: {
					Name:   "ssh",
					Inside: 22,
				},
			},
		},
		kClient: kfake.NewSimpleClientset(),
		want: []*corev1.Service{{
			TypeMeta: metav1.TypeMeta{
				Kind:       "Service",
				APIVersion: "v1",
			},
			ObjectMeta: metav1.ObjectMeta{
				Name:      "service-dev1",
				Namespace: "test",
				Labels:    map[string]string{"pod": "dev1"},
			},
			Spec: corev1.ServiceSpec{
				Ports: []corev1.ServicePort{{
					Name:       "ssh",
					Protocol:   "TCP",
					Port:       22,
					TargetPort: intstr.FromInt(22),
					NodePort:   0,
				}},
				Selector: map[string]string{"app": "dev1"},
				Type:     "LoadBalancer",
			},
		}},
	}, {
		desc: "services valid multiple mappings",
		node: &topopb.Node{
			Name: "dev2",
			Type: topopb.Node_Type(1001),
			Services: map[uint32]*topopb.Service{
				9339: {
					Name:    "gnmi",
					Inside:  9339,
					Outside: 9339,
				},
				9337: {
					Name:    "gnoi",
					Inside:  9339,
					Outside: 9337,
				},
			},
		},
		kClient: kfake.NewSimpleClientset(),
		want: []*corev1.Service{{
			TypeMeta: metav1.TypeMeta{
				Kind:       "Service",
				APIVersion: "v1",
			},
			ObjectMeta: metav1.ObjectMeta{
				Name:      "service-dev2",
				Namespace: "test",
				Labels:    map[string]string{"pod": "dev2"},
			},
			Spec: corev1.ServiceSpec{
				Ports: []corev1.ServicePort{{
					Name:       "gnmi",
					Protocol:   "TCP",
					Port:       9339,
					TargetPort: intstr.FromInt(9339),
					NodePort:   0,
				}, {
					Name:       "gnoi",
					Protocol:   "TCP",
					Port:       9337,
					TargetPort: intstr.FromInt(9339),
					NodePort:   0,
				}},
				Selector: map[string]string{"app": "dev2"},
				Type:     "LoadBalancer",
			},
		}},
	}, {
		desc: "failed create duplicate",
		node: &topopb.Node{
			Name: "dev1",
			Type: topopb.Node_Type(1001),
			Services: map[uint32]*topopb.Service{
				22: {
					Name:   "ssh",
					Inside: 22,
				},
			},
		},
		kClient: kfake.NewSimpleClientset(&corev1.Service{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "service-dev1",
				Namespace: "test",
			},
		}),
		wantCreateErr: `"service-dev1" already exists`,
	}}
	for _, tt := range tests {
		t.Run(tt.desc, func(t *testing.T) {
			n := &Impl{
				Namespace:  "test",
				KubeClient: tt.kClient,
				RestConfig: &rest.Config{},
				Proto:      tt.node,
				BasePath:   "",
				Kubecfg:    "",
			}
			err := n.CreateService(context.Background())
			if s := errdiff.Check(err, tt.wantCreateErr); s != "" {
				t.Fatalf("CreateService() failed: %s", s)
			}
			if tt.wantServiceErr != "" {
				return
			}

			got, err := n.Services(context.Background())
			if s := errdiff.Check(err, tt.wantServiceErr); s != "" {
				t.Fatalf("Services() failed: %s", s)
			}
			if tt.wantCreateErr != "" {
				return
			}
			if s := cmp.Diff(tt.want, got,
				cmpopts.SortSlices(func(a, b corev1.ServicePort) bool {
					return a.Name < b.Name
				})); s != "" {
				t.Fatalf("Services() failed: %s", s)
			}
		})
	}
}
