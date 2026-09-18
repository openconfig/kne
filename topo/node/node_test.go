package node

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"testing"

	"github.com/google/go-cmp/cmp"
	"github.com/google/go-cmp/cmp/cmpopts"
	"github.com/openconfig/gnmi/errdiff"
	topopb "github.com/openconfig/kne/proto/topo"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/util/intstr"
	kfake "k8s.io/client-go/kubernetes/fake"
	"k8s.io/client-go/rest"
	ktesting "k8s.io/client-go/testing"
	"k8s.io/utils/pointer"
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

var registerVendorsOnce sync.Once

func TestReset(t *testing.T) {
	registerVendorsOnce.Do(func() {
		Vendor(topopb.Vendor(1001), NewR)
		Vendor(topopb.Vendor(1002), NewNR)
	})
	n, err := New("test", &topopb.Node{Vendor: topopb.Vendor(1001)}, nil, nil, "", "")
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
	nr, err := New("test", &topopb.Node{Vendor: topopb.Vendor(1002)}, nil, nil, "", "")
	if err != nil {
		t.Fatalf("failed to create node: %v", err)
	}
	_, ok = nr.(Resetter)
	if ok {
		t.Errorf("Not-Resettable node type asserted to resetter")
	}
}

func TestCreateConfig(t *testing.T) {
	ctx := context.Background()

	origTempCfgDir := tempCfgDir
	defer func() {
		tempCfgDir = origTempCfgDir
	}()
	tempCfgDir = t.TempDir()

	tests := []struct {
		desc    string
		node    *topopb.Node
		wantErr string
		want    *corev1.Volume
		wantCM  *corev1.ConfigMap
	}{{
		desc: "small config from file",
		node: &topopb.Node{
			Name:   "dev1",
			Vendor: topopb.Vendor(1001),
			Config: &topopb.Config{
				ConfigFile: "test.cfg",
				ConfigData: &topopb.Config_File{
					File: "testdata/small.cfg",
				},
			},
		},
		want: &corev1.Volume{
			Name: ConfigVolumeName,
			VolumeSource: corev1.VolumeSource{
				ConfigMap: &corev1.ConfigMapVolumeSource{
					LocalObjectReference: corev1.LocalObjectReference{
						Name: "dev1-config",
					},
				},
			},
		},
		wantCM: &corev1.ConfigMap{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "dev1-config",
				Namespace: "test",
			},
			Data: map[string]string{
				"test.cfg": "test config\n",
			},
		},
	}, {
		desc: "small config from data",
		node: &topopb.Node{
			Name:   "dev1",
			Vendor: topopb.Vendor(1001),
			Config: &topopb.Config{
				ConfigFile: "test.cfg",
				ConfigData: &topopb.Config_Data{
					Data: []byte("test config\n"),
				},
			},
		},
		want: &corev1.Volume{
			Name: ConfigVolumeName,
			VolumeSource: corev1.VolumeSource{
				ConfigMap: &corev1.ConfigMapVolumeSource{
					LocalObjectReference: corev1.LocalObjectReference{
						Name: "dev1-config",
					},
				},
			},
		},
		wantCM: &corev1.ConfigMap{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "dev1-config",
				Namespace: "test",
			},
			Data: map[string]string{
				"test.cfg": "test config\n",
			},
		},
	}, {
		desc: "large config from file",
		node: &topopb.Node{
			Name:   "dev1",
			Vendor: topopb.Vendor(1001),
			Config: &topopb.Config{
				ConfigFile: "test.cfg",
				ConfigData: &topopb.Config_File{
					File: "testdata/large.cfg",
				},
			},
		},
		want: &corev1.Volume{
			Name: ConfigVolumeName,
			VolumeSource: corev1.VolumeSource{
				HostPath: &corev1.HostPathVolumeSource{
					Path: filepath.Join(tempCfgDir, "kne-dev1-config-*.cfg"),
				},
			},
		},
	}, {
		desc: "config file dne",
		node: &topopb.Node{
			Name:   "dev1",
			Vendor: topopb.Vendor(1001),
			Config: &topopb.Config{
				ConfigFile: "test.cfg",
				ConfigData: &topopb.Config_File{
					File: "testdata/dne.cfg",
				},
			},
		},
		wantErr: "open testdata/dne.cfg: no such file",
	}}
	for _, tt := range tests {
		t.Run(tt.desc, func(t *testing.T) {
			n := &Impl{
				Namespace:  "test",
				KubeClient: kfake.NewSimpleClientset(),
				RestConfig: &rest.Config{},
				Proto:      tt.node,
				BasePath:   "",
				Kubecfg:    "",
			}
			got, err := n.CreateConfig(ctx)
			if s := errdiff.Check(err, tt.wantErr); s != "" {
				t.Errorf("CreateConfig() failed: %s", s)
			}
			if tt.wantErr != "" {
				return
			}
			if s := cmp.Diff(tt.want, got, cmpopts.IgnoreFields(corev1.HostPathVolumeSource{}, "Path")); s != "" {
				t.Errorf("CreateConfig() unexpected diff: %s", s)
			}
			switch vs := got.VolumeSource; {
			case vs.HostPath != nil:
				if _, err := os.Stat(vs.HostPath.Path); err != nil {
					t.Errorf("CreateConfig() did not create the expected file: %v", err)
				}
			case vs.ConfigMap != nil:
				gotCM, err := n.KubeClient.CoreV1().ConfigMaps(n.Namespace).Get(ctx, vs.ConfigMap.LocalObjectReference.Name, metav1.GetOptions{})
				if err != nil {
					t.Errorf("CreateConfig() did not create the expected configmap: %v", err)
				}
				if s := cmp.Diff(tt.wantCM, gotCM); s != "" {
					t.Errorf("CreateConfig() created configmap unexpected diff: %s", s)
				}
			}
		})
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
		desc:    "no services",
		node:    &topopb.Node{Name: "dev1", Vendor: topopb.Vendor(1001)},
		kClient: kfake.NewSimpleClientset(),
	}, {
		desc: "services valid",
		node: &topopb.Node{
			Name:   "dev1",
			Vendor: topopb.Vendor(1001),
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
				Selector:                      map[string]string{"app": "dev1"},
				Type:                          "LoadBalancer",
				AllocateLoadBalancerNodePorts: pointer.Bool(false),
			},
		}},
	}, {
		desc: "services valid multiple mappings",
		node: &topopb.Node{
			Name:   "dev2",
			Vendor: topopb.Vendor(1001),
			Services: map[uint32]*topopb.Service{
				9339: {
					Name:   "gnmi",
					Inside: 9339,
				},
				9337: {
					Name:   "gnoi",
					Inside: 9339,
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
				Selector:                      map[string]string{"app": "dev2"},
				Type:                          "LoadBalancer",
				AllocateLoadBalancerNodePorts: pointer.Bool(false),
			},
		}},
	}, {
		desc: "nodeport service valid",
		node: &topopb.Node{
			Name:   "dev-nodeport",
			Vendor: topopb.Vendor(1001),
			Services: map[uint32]*topopb.Service{
				50058: {
					Name:   "wire",
					Inside: 50058,
					Type:   topopb.Service_NODE_PORT,
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
				Name:      "service-dev-nodeport-nodeport",
				Namespace: "test",
				Labels:    map[string]string{"pod": "dev-nodeport"},
			},
			Spec: corev1.ServiceSpec{
				Ports: []corev1.ServicePort{{
					Name:       "wire",
					Protocol:   "TCP",
					Port:       50058,
					TargetPort: intstr.FromInt(50058),
					NodePort:   0,
				}},
				Selector: map[string]string{"app": "dev-nodeport"},
				Type:     "NodePort",
			},
		}},
	}, {
		desc: "nodeport service with static port",
		node: &topopb.Node{
			Name:   "dev-nodeport-static",
			Vendor: topopb.Vendor(1001),
			Services: map[uint32]*topopb.Service{
				50058: {
					Name:     "wire",
					Inside:   50058,
					Type:     topopb.Service_NODE_PORT,
					NodePort: 30058,
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
				Name:      "service-dev-nodeport-static-nodeport",
				Namespace: "test",
				Labels:    map[string]string{"pod": "dev-nodeport-static"},
			},
			Spec: corev1.ServiceSpec{
				Ports: []corev1.ServicePort{{
					Name:       "wire",
					Protocol:   "TCP",
					Port:       50058,
					TargetPort: intstr.FromInt(50058),
					NodePort:   30058,
				}},
				Selector: map[string]string{"app": "dev-nodeport-static"},
				Type:     "NodePort",
			},
		}},
	}, {
		desc: "clusterip service valid",
		node: &topopb.Node{
			Name:   "dev-clusterip",
			Vendor: topopb.Vendor(1001),
			Services: map[uint32]*topopb.Service{
				8080: {
					Name:   "http",
					Inside: 8080,
					Type:   topopb.Service_CLUSTER_IP,
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
				Name:      "service-dev-clusterip-clusterip",
				Namespace: "test",
				Labels:    map[string]string{"pod": "dev-clusterip"},
			},
			Spec: corev1.ServiceSpec{
				Ports: []corev1.ServicePort{{
					Name:       "http",
					Protocol:   "TCP",
					Port:       8080,
					TargetPort: intstr.FromInt(8080),
					NodePort:   0,
				}},
				Selector: map[string]string{"app": "dev-clusterip"},
				Type:     "ClusterIP",
			},
		}},
	}, {
		desc: "mixed service types valid",
		node: &topopb.Node{
			Name:   "dev-mixed",
			Vendor: topopb.Vendor(1001),
			Services: map[uint32]*topopb.Service{
				22: {
					Name:   "ssh",
					Inside: 22,
				},
				50058: {
					Name:     "wire",
					Inside:   50058,
					Type:     topopb.Service_NODE_PORT,
					NodePort: 30058,
				},
				8080: {
					Name:   "http",
					Inside: 8080,
					Type:   topopb.Service_CLUSTER_IP,
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
				Name:      "service-dev-mixed",
				Namespace: "test",
				Labels:    map[string]string{"pod": "dev-mixed"},
			},
			Spec: corev1.ServiceSpec{
				Ports: []corev1.ServicePort{{
					Name:       "ssh",
					Protocol:   "TCP",
					Port:       22,
					TargetPort: intstr.FromInt(22),
					NodePort:   0,
				}},
				Selector:                      map[string]string{"app": "dev-mixed"},
				Type:                          "LoadBalancer",
				AllocateLoadBalancerNodePorts: pointer.Bool(false),
			},
		}, {
			TypeMeta: metav1.TypeMeta{
				Kind:       "Service",
				APIVersion: "v1",
			},
			ObjectMeta: metav1.ObjectMeta{
				Name:      "service-dev-mixed-nodeport",
				Namespace: "test",
				Labels:    map[string]string{"pod": "dev-mixed"},
			},
			Spec: corev1.ServiceSpec{
				Ports: []corev1.ServicePort{{
					Name:       "wire",
					Protocol:   "TCP",
					Port:       50058,
					TargetPort: intstr.FromInt(50058),
					NodePort:   30058,
				}},
				Selector: map[string]string{"app": "dev-mixed"},
				Type:     "NodePort",
			},
		}, {
			TypeMeta: metav1.TypeMeta{
				Kind:       "Service",
				APIVersion: "v1",
			},
			ObjectMeta: metav1.ObjectMeta{
				Name:      "service-dev-mixed-clusterip",
				Namespace: "test",
				Labels:    map[string]string{"pod": "dev-mixed"},
			},
			Spec: corev1.ServiceSpec{
				Ports: []corev1.ServicePort{{
					Name:       "http",
					Protocol:   "TCP",
					Port:       8080,
					TargetPort: intstr.FromInt(8080),
					NodePort:   0,
				}},
				Selector: map[string]string{"app": "dev-mixed"},
				Type:     "ClusterIP",
			},
		}},
	}, {
		desc: "failed create duplicate",
		node: &topopb.Node{
			Name:   "dev1",
			Vendor: topopb.Vendor(1001),
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
	},
	}
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

			got, err := n.Services(context.Background())
			if s := errdiff.Check(err, tt.wantServiceErr); s != "" {
				t.Fatalf("Services() failed: %s", s)
			}
			if tt.wantCreateErr != "" {
				return
			}
			if s := cmp.Diff(tt.want, got,
				cmpopts.SortSlices(func(a, b *corev1.Service) bool {
					return a.Name < b.Name
				}),
				cmpopts.SortSlices(func(a, b corev1.ServicePort) bool {
					return a.Name < b.Name
				})); s != "" {
				t.Fatalf("Services() failed: %s", s)
			}
		})
	}
}

func TestDeleteServiceErrorHandling(t *testing.T) {
	ctx := context.Background()

	// 1. DaemonSet delete forbidden should return error and not be swallowed
	kClient := kfake.NewSimpleClientset()
	kClient.PrependReactor("delete", "daemonsets", func(action ktesting.Action) (handled bool, ret runtime.Object, err error) {
		return true, nil, apierrors.NewForbidden(action.GetResource().GroupResource(), "v6proxy-dev1", fmt.Errorf("access denied"))
	})
	n := &Impl{
		Namespace:  "test",
		KubeClient: kClient,
		Proto:      &topopb.Node{Name: "dev1"},
	}
	err := n.DeleteService(ctx)
	if err == nil {
		t.Fatalf("expected error from DeleteService when daemonset delete is forbidden, got nil")
	}

	// 2. NotFound on both daemonsets and services should return nil
	kClient2 := kfake.NewSimpleClientset()
	n2 := &Impl{
		Namespace:  "test",
		KubeClient: kClient2,
		Proto:      &topopb.Node{Name: "dev1"},
	}
	if err := n2.DeleteService(ctx); err != nil {
		t.Fatalf("expected DeleteService to return nil on NotFound, got: %v", err)
	}

	// 3. Impl.Delete should propagate the error
	if err := n.Delete(ctx); err == nil {
		t.Fatalf("expected Impl.Delete to return error when DeleteService fails, got nil")
	}
}

func TestCreateServiceV6HostProxy(t *testing.T) {
	ctx := context.Background()
	kClient := kfake.NewSimpleClientset()
	n := &Impl{
		Namespace:  "test",
		KubeClient: kClient,
		Proto: &topopb.Node{
			Name:   "dev-v6",
			Vendor: topopb.Vendor(1001),
			Services: map[uint32]*topopb.Service{
				50058: {
					Name:        "wire",
					Inside:      50058,
					Type:        topopb.Service_NODE_PORT,
					NodePort:    30058,
					V6HostProxy: true,
				},
				8080: {
					Name:        "http",
					Inside:      8080,
					Type:        topopb.Service_CLUSTER_IP,
					V6HostProxy: true, // Should not create a proxy container because NodePort is 0
				},
			},
		},
	}

	if err := n.CreateService(ctx); err != nil {
		t.Fatalf("CreateService() failed: %v", err)
	}

	// Verify DaemonSet was created
	dsList, err := kClient.AppsV1().DaemonSets("test").List(ctx, metav1.ListOptions{})
	if err != nil {
		t.Fatalf("failed to list daemonsets: %v", err)
	}
	if len(dsList.Items) != 1 {
		t.Fatalf("expected 1 daemonset, got %d", len(dsList.Items))
	}
	ds := dsList.Items[0]
	if ds.Name != "v6proxy-dev-v6" {
		t.Errorf("daemonset name = %q, want %q", ds.Name, "v6proxy-dev-v6")
	}
	if len(ds.OwnerReferences) == 0 || ds.OwnerReferences[0].Kind != "Service" {
		t.Errorf("expected OwnerReference to Service, got: %+v", ds.OwnerReferences)
	}
	if ds.Spec.Template.Spec.DNSPolicy != corev1.DNSClusterFirstWithHostNet {
		t.Errorf("DNSPolicy = %v, want %v", ds.Spec.Template.Spec.DNSPolicy, corev1.DNSClusterFirstWithHostNet)
	}
	if len(ds.Spec.Template.Spec.Tolerations) == 0 {
		t.Errorf("expected tolerations on DaemonSet pod spec")
	}
	if len(ds.Spec.Template.Spec.Containers) != 1 {
		t.Fatalf("expected 1 proxy container (for NodePort 30058 only), got %d", len(ds.Spec.Template.Spec.Containers))
	}
	c := ds.Spec.Template.Spec.Containers[0]
	if c.Image != DefaultV6ProxyImage {
		t.Errorf("container image = %q, want %q", c.Image, DefaultV6ProxyImage)
	}
	if c.SecurityContext == nil || c.SecurityContext.RunAsNonRoot == nil || !*c.SecurityContext.RunAsNonRoot {
		t.Errorf("expected SecurityContext with RunAsNonRoot=true")
	}
	if len(c.Resources.Requests) == 0 || len(c.Resources.Limits) == 0 {
		t.Errorf("expected explicit Requests and Limits on proxy container")
	}
	wantArg := fmt.Sprintf("TCP6-LISTEN:30058,fork,reuseaddr,max-children=%d", defaultSocatMaxChildren)
	if c.Args[0] != wantArg {
		t.Errorf("container args[0] = %q, want %q", c.Args[0], wantArg)
	}
}

func TestCreateServiceDaemonSetFailureRollback(t *testing.T) {
	ctx := context.Background()
	kClient := kfake.NewSimpleClientset()
	kClient.PrependReactor("create", "daemonsets", func(action ktesting.Action) (handled bool, ret runtime.Object, err error) {
		return true, nil, fmt.Errorf("injected daemonset creation failure")
	})
	n := &Impl{
		Namespace:  "test",
		KubeClient: kClient,
		Proto: &topopb.Node{
			Name:   "dev-fail",
			Vendor: topopb.Vendor(1001),
			Services: map[uint32]*topopb.Service{
				50058: {
					Name:        "wire",
					Inside:      50058,
					Type:        topopb.Service_NODE_PORT,
					NodePort:    30058,
					V6HostProxy: true,
				},
			},
		},
	}

	err := n.CreateService(ctx)
	if err == nil {
		t.Fatalf("expected CreateService to fail when DaemonSet creation fails")
	}

	// Verify Services were cleaned up on error (rollback)
	svcList, err := kClient.CoreV1().Services("test").List(ctx, metav1.ListOptions{})
	if err != nil {
		t.Fatalf("failed to list services: %v", err)
	}
	if len(svcList.Items) != 0 {
		t.Errorf("expected 0 services after rollback, found %d", len(svcList.Items))
	}
}

func TestValidateConstraints(t *testing.T) {
	tests := []struct {
		desc             string
		node             *topopb.Node
		wantErr          string
		constraintValues map[string]int
	}{
		{
			desc: "Invalid case - constraint value is greater than upper bound",
			node: &topopb.Node{
				Name: "node1",
				HostConstraints: []*topopb.HostConstraint{
					{
						Constraint: &topopb.HostConstraint_KernelConstraint{
							KernelConstraint: &topopb.KernelParam{
								Name:           "fs.inotify.max_user_instances",
								ConstraintType: &topopb.KernelParam_BoundedInteger{BoundedInteger: &topopb.BoundedInteger{MaxValue: 1000}},
							},
						},
					},
				},
			},
			constraintValues: map[string]int{"fs.inotify.max_user_instances": 1500},
			wantErr:          "failed to validate kernel constraint error: invalid bounded integer constraint. min: 0 max 1000 constraint data 1500",
		},
		{
			desc: "Invalid case - constraint value is lesser than lower bound",
			node: &topopb.Node{
				Name: "node1",
				HostConstraints: []*topopb.HostConstraint{
					{
						Constraint: &topopb.HostConstraint_KernelConstraint{
							KernelConstraint: &topopb.KernelParam{
								Name:           "fs.inotify.max_user_instances",
								ConstraintType: &topopb.KernelParam_BoundedInteger{BoundedInteger: &topopb.BoundedInteger{MinValue: 10, MaxValue: 100}},
							},
						},
					},
				},
			},
			constraintValues: map[string]int{"fs.inotify.max_user_instances": 5},
			wantErr:          "failed to validate kernel constraint error: invalid bounded integer constraint. min: 10 max 100 constraint data 5",
		},
		{
			desc: "Invalid case - constraint bounds is invalid upper bound is less than lower bound",
			node: &topopb.Node{
				Name: "node1",
				HostConstraints: []*topopb.HostConstraint{
					{
						Constraint: &topopb.HostConstraint_KernelConstraint{
							KernelConstraint: &topopb.KernelParam{
								Name:           "fs.inotify.max_user_instances",
								ConstraintType: &topopb.KernelParam_BoundedInteger{BoundedInteger: &topopb.BoundedInteger{MinValue: 10, MaxValue: 1}},
							},
						},
					},
				},
			},
			constraintValues: map[string]int{"fs.inotify.max_user_instances": 5},
			wantErr:          "failed to validate kernel constraint error: invalid bounds. Max value 1 is less than min value 10",
		},
		{
			desc: "Valid constraint",
			node: &topopb.Node{
				Name: "node1",
				HostConstraints: []*topopb.HostConstraint{
					{
						Constraint: &topopb.HostConstraint_KernelConstraint{
							KernelConstraint: &topopb.KernelParam{
								Name:           "fs.inotify.max_user_instances",
								ConstraintType: &topopb.KernelParam_BoundedInteger{BoundedInteger: &topopb.BoundedInteger{MinValue: 1, MaxValue: 1000}},
							},
						},
					},
				},
			},
			constraintValues: map[string]int{"fs.inotify.max_user_instances": 500},
		},
	}

	for _, tt := range tests {
		t.Run(tt.desc, func(t *testing.T) {
			n := &Impl{
				Proto: tt.node,
			}

			origkernelConstraintValue := kernelConstraintValue
			defer func() {
				kernelConstraintValue = origkernelConstraintValue
			}()
			kernelConstraintValue = func(constraint string) (int, error) {
				return tt.constraintValues[constraint], nil
			}
			err := n.ValidateConstraints()
			if d := errdiff.Substring(err, tt.wantErr); d != "" {
				t.Fatalf("ValidateConstraints() failed: %s", d)
			}
		})
	}
}

func TestServiceReadinessProbe(t *testing.T) {
	tests := []struct {
		desc string
		node *topopb.Node
		want *corev1.Probe
	}{
		{
			desc: "nil node",
			node: nil,
			want: nil,
		},
		{
			desc: "no services",
			node: &topopb.Node{},
			want: nil,
		},
		{
			desc: "ssh service",
			node: &topopb.Node{
				Services: map[uint32]*topopb.Service{
					22: {
						Name:   "ssh",
						Inside: 22,
					},
				},
			},
			want: &corev1.Probe{
				ProbeHandler: corev1.ProbeHandler{
					TCPSocket: &corev1.TCPSocketAction{
						Port: intstr.FromInt(22),
					},
				},
				InitialDelaySeconds: 10,
				PeriodSeconds:       10,
				FailureThreshold:    60,
			},
		},
		{
			desc: "ssh service custom inside port",
			node: &topopb.Node{
				Services: map[uint32]*topopb.Service{
					22: {
						Name:   "ssh",
						Inside: 2222,
					},
				},
			},
			want: &corev1.Probe{
				ProbeHandler: corev1.ProbeHandler{
					TCPSocket: &corev1.TCPSocketAction{
						Port: intstr.FromInt(2222),
					},
				},
				InitialDelaySeconds: 10,
				PeriodSeconds:       10,
				FailureThreshold:    60,
			},
		},
		{
			desc: "ssh service in names slice",
			node: &topopb.Node{
				Services: map[uint32]*topopb.Service{
					22: {
						Names:  []string{"ssh", "cli"},
						Inside: 22,
					},
				},
			},
			want: &corev1.Probe{
				ProbeHandler: corev1.ProbeHandler{
					TCPSocket: &corev1.TCPSocketAction{
						Port: intstr.FromInt(22),
					},
				},
				InitialDelaySeconds: 10,
				PeriodSeconds:       10,
				FailureThreshold:    60,
			},
		},
		{
			desc: "gnmi service only",
			node: &topopb.Node{
				Services: map[uint32]*topopb.Service{
					9339: {
						Name:   "gnmi",
						Inside: 57400,
					},
				},
			},
			want: &corev1.Probe{
				ProbeHandler: corev1.ProbeHandler{
					TCPSocket: &corev1.TCPSocketAction{
						Port: intstr.FromInt(57400),
					},
				},
				InitialDelaySeconds: 10,
				PeriodSeconds:       10,
				FailureThreshold:    60,
			},
		},
		{
			desc: "both ssh and gnmi prefers ssh",
			node: &topopb.Node{
				Services: map[uint32]*topopb.Service{
					22: {
						Name:   "ssh",
						Inside: 22,
					},
					9339: {
						Names:  []string{"gnmi", "gnoi"},
						Inside: 57400,
					},
				},
			},
			want: &corev1.Probe{
				ProbeHandler: corev1.ProbeHandler{
					TCPSocket: &corev1.TCPSocketAction{
						Port: intstr.FromInt(22),
					},
				},
				InitialDelaySeconds: 10,
				PeriodSeconds:       10,
				FailureThreshold:    60,
			},
		},
		{
			desc: "unsupported service only",
			node: &topopb.Node{
				Services: map[uint32]*topopb.Service{
					179: {
						Name:   "bgp",
						Inside: 179,
					},
				},
			},
			want: nil,
		},
		{
			desc: "service with zero inside falls back to map key",
			node: &topopb.Node{
				Services: map[uint32]*topopb.Service{
					22: {
						Name:   "ssh",
						Inside: 0,
					},
				},
			},
			want: &corev1.Probe{
				ProbeHandler: corev1.ProbeHandler{
					TCPSocket: &corev1.TCPSocketAction{
						Port: intstr.FromInt(22),
					},
				},
				InitialDelaySeconds: 10,
				PeriodSeconds:       10,
				FailureThreshold:    60,
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.desc, func(t *testing.T) {
			got := ServiceReadinessProbe(tt.node)
			if diff := cmp.Diff(tt.want, got); diff != "" {
				t.Errorf("ServiceReadinessProbe() mismatch (-want +got):\n%s", diff)
			}
		})
	}
}

func TestV6HostProxyDaemonSet(t *testing.T) {
	ctx := context.Background()
	kClient := kfake.NewSimpleClientset()
	node := &topopb.Node{
		Name:   "dev-v6proxy",
		Vendor: topopb.Vendor(1001),
		Services: map[uint32]*topopb.Service{
			50058: {
				Name:        "wire",
				Inside:      50058,
				Type:        topopb.Service_NODE_PORT,
				NodePort:    30058,
				V6HostProxy: true,
			},
			50059: {
				Name:        "noproxy",
				Inside:      50059,
				Type:        topopb.Service_NODE_PORT,
				NodePort:    30059,
				V6HostProxy: false,
			},
		},
	}
	n := &Impl{
		Namespace:  "test",
		KubeClient: kClient,
		RestConfig: &rest.Config{},
		Proto:      node,
	}

	if err := n.CreateService(ctx); err != nil {
		t.Fatalf("CreateService() failed: %v", err)
	}

	ds, err := kClient.AppsV1().DaemonSets("test").Get(ctx, "v6proxy-dev-v6proxy", metav1.GetOptions{})
	if err != nil {
		t.Fatalf("failed to get expected daemonset: %v", err)
	}

	wantDS := &appsv1.DaemonSet{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "v6proxy-dev-v6proxy",
			Namespace: "test",
			Labels: map[string]string{
				"app":  "v6proxy-dev-v6proxy",
				"pod":  "dev-v6proxy",
				"topo": "test",
			},
			OwnerReferences: []metav1.OwnerReference{
				{
					APIVersion:         "v1",
					Kind:               "Service",
					Name:               "service-dev-v6proxy-nodeport",
					BlockOwnerDeletion: pointer.Bool(true),
					Controller:         pointer.Bool(true),
				},
			},
		},
		Spec: appsv1.DaemonSetSpec{
			Selector: &metav1.LabelSelector{
				MatchLabels: map[string]string{
					"app":  "v6proxy-dev-v6proxy",
					"pod":  "dev-v6proxy",
					"topo": "test",
				},
			},
			Template: corev1.PodTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{
					Labels: map[string]string{
						"app":  "v6proxy-dev-v6proxy",
						"pod":  "dev-v6proxy",
						"topo": "test",
					},
				},
				Spec: corev1.PodSpec{
					HostNetwork: true,
					DNSPolicy:   corev1.DNSClusterFirstWithHostNet,
					Tolerations: []corev1.Toleration{
						{
							Operator: corev1.TolerationOpExists,
						},
					},
					Containers: []corev1.Container{{
						Name:  "socat-30058",
						Image: DefaultV6ProxyImage,
						Args: []string{
							fmt.Sprintf("TCP6-LISTEN:30058,fork,reuseaddr,max-children=%d", defaultSocatMaxChildren),
							"TCP4:127.0.0.1:30058",
						},
						ImagePullPolicy: corev1.PullIfNotPresent,
						SecurityContext: &corev1.SecurityContext{
							AllowPrivilegeEscalation: pointer.Bool(false),
							Capabilities: &corev1.Capabilities{
								Drop: []corev1.Capability{"ALL"},
							},
							RunAsNonRoot: pointer.Bool(true),
						},
						Resources: corev1.ResourceRequirements{
							Requests: corev1.ResourceList{
								corev1.ResourceCPU:    resource.MustParse("50m"),
								corev1.ResourceMemory: resource.MustParse("32Mi"),
							},
							Limits: corev1.ResourceList{
								corev1.ResourceCPU:    resource.MustParse("100m"),
								corev1.ResourceMemory: resource.MustParse("64Mi"),
							},
						},
					}},
					TerminationGracePeriodSeconds: pointer.Int64(0),
				},
			},
		},
	}

	if s := cmp.Diff(wantDS, ds); s != "" {
		t.Errorf("DaemonSet diff (-want +got):\n%s", s)
	}

	// Verify DeleteService deletes the DaemonSet
	if err := n.DeleteService(ctx); err != nil {
		t.Fatalf("DeleteService() failed: %v", err)
	}

	if _, err := kClient.AppsV1().DaemonSets("test").Get(ctx, "v6proxy-dev-v6proxy", metav1.GetOptions{}); err == nil {
		t.Errorf("expected daemonset to be deleted, but still found")
	}
}
