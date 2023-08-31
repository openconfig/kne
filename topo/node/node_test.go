package node

import (
	"context"
	"os"
	"path/filepath"
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
	Vendor(topopb.Vendor(1001), NewR)
	Vendor(topopb.Vendor(1002), NewNR)
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
		desc:           "no services",
		node:           &topopb.Node{Name: "dev1", Vendor: topopb.Vendor(1001)},
		kClient:        kfake.NewSimpleClientset(),
		wantServiceErr: `"service-dev1" not found`,
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
				Selector: map[string]string{"app": "dev1"},
				Type:     "LoadBalancer",
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
				Selector: map[string]string{"app": "dev2"},
				Type:     "LoadBalancer",
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

func TestValidateConstraints(t *testing.T) {
	tests := []struct {
		desc             string
		node             *topopb.Node
		wantErr          string
		constraintValues map[string]int
	}{
		{
			desc: "Invalid case - contraint value is greater than upper bound",
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
			desc: "Invalid case - constraint bounds is invalid uooer bound is less tha lower bound",
			node: &topopb.Node{
				Name: "node1",
				HostConstraints: []*topopb.HostConstraint{
					{
						Constraint: &topopb.HostConstraint_KernelConstraint{
							KernelConstraint: &topopb.KernelParam{
								Name:           "fs.inotify.max_user_instances",
								ConstraintType: &topopb.KernelParam_BoundedInteger{BoundedInteger: &topopb.BoundedInteger{MinValue: 10}},
							},
						},
					},
				},
			},
			constraintValues: map[string]int{"fs.inotify.max_user_instances": 2000},
			wantErr:          "failed to validate kernel constraint error: invalid bounds. Max value 0 is less than min value 10",
		},
		{
			desc: "Valid constraint",
			node: &topopb.Node{
				Name: "",
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
			kernelConstraintValue = func(basePath, constraint string) (int, error) {
				return tt.constraintValues[constraint], nil
			}
			err := n.ValidateConstraints()
			if d := errdiff.Substring(err, tt.wantErr); d != "" {
				t.Fatalf("ValidateConstraints() failed: %s", d)
			}
		})
	}
}
