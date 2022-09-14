package v1beta1

import (
	"context"
	"testing"

	"github.com/google/go-cmp/cmp"
	"github.com/google/go-cmp/cmp/cmpopts"
	"github.com/h-fam/errdiff"
	metallbv1 "go.universe.tf/metallb/api/v1beta1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/watch"
)

var (
	l2ObjNew = &metallbv1.L2Advertisement{
		TypeMeta: metav1.TypeMeta{
			Kind:       "L2Advertisement",
			APIVersion: "metallb.io/v1beta1",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:       "l2ObjNew",
			Namespace:  "test",
			Generation: 1,
		},
		Status: metallbv1.L2AdvertisementStatus{},
		Spec: metallbv1.L2AdvertisementSpec{
			IPAddressPools: []string{"test-pool1"},
		},
	}
	l2Obj1 = &metallbv1.L2Advertisement{
		TypeMeta: metav1.TypeMeta{
			Kind:       "L2Advertisement",
			APIVersion: "metallb.io/v1beta1",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:       "l2Obj1",
			Namespace:  "test",
			Generation: 1,
		},
		Status: metallbv1.L2AdvertisementStatus{},
		Spec: metallbv1.L2AdvertisementSpec{
			IPAddressPools: []string{"test-pool1"},
		},
	}
	l2Obj2 = &metallbv1.L2Advertisement{
		TypeMeta: metav1.TypeMeta{
			Kind:       "L2Advertisement",
			APIVersion: "metallb.io/v1beta1",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:       "l2Obj2",
			Namespace:  "test",
			Generation: 1,
		},
		Status: metallbv1.L2AdvertisementStatus{},
		Spec: metallbv1.L2AdvertisementSpec{
			IPAddressPools: []string{"test-pool2"},
		},
	}
)

func TestL2AdvertisementL2AdvertisementCreate(t *testing.T) {
	cs := setUp(t)
	objWithoutTypeMetaOut := l2Obj1.DeepCopy()
	objWithoutTypeMetaOut.Name = "newObjWithoutTypeMeta"
	objWithoutTypeMetaIn := objWithoutTypeMetaOut.DeepCopy()
	objWithoutTypeMetaIn.TypeMeta.Reset()
	tests := []struct {
		desc    string
		in      *metallbv1.L2Advertisement
		want    *metallbv1.L2Advertisement
		wantErr string
	}{{
		desc:    "already exists",
		in:      l2Obj1,
		wantErr: "already exists",
	}, {
		desc: "success",
		in:   l2ObjNew,
		want: l2ObjNew,
	}, {
		desc: "success without typemeta",
		in:   objWithoutTypeMetaIn,
		want: objWithoutTypeMetaOut,
	}}
	for _, tt := range tests {
		t.Run(tt.desc, func(t *testing.T) {
			tc := cs.L2Advertisement("test")
			got, err := tc.Create(context.Background(), tt.in, metav1.CreateOptions{})
			if s := errdiff.Substring(err, tt.wantErr); s != "" {
				t.Fatalf("unexpected error: %s", s)
			}
			if tt.wantErr != "" {
				return
			}
			if s := cmp.Diff(tt.want, got); s != "" {
				t.Fatalf("Create(%+v) failed: %s", tt.want, s)
			}
		})
	}
}

func TestL2AdvertisementList(t *testing.T) {
	cs := setUp(t)
	tests := []struct {
		desc    string
		want    *metallbv1.L2AdvertisementList
		wantErr string
	}{{
		desc: "success",
		want: &metallbv1.L2AdvertisementList{
			Items: []metallbv1.L2Advertisement{*l2Obj1, *l2Obj2},
		},
	}}
	for _, tt := range tests {
		t.Run(tt.desc, func(t *testing.T) {
			tc := cs.L2Advertisement("test")
			got, err := tc.List(context.Background(), metav1.ListOptions{})
			if s := errdiff.Substring(err, tt.wantErr); s != "" {
				t.Fatalf("unexpected error: %s", s)
			}
			if tt.wantErr != "" {
				return
			}
			if s := cmp.Diff(tt.want, got, cmpopts.IgnoreFields(metallbv1.L2AdvertisementList{}, "TypeMeta")); s != "" {
				t.Fatalf("List() failed: %s", s)
			}
		})
	}
}

func TestL2AdvertisementGet(t *testing.T) {
	cs := setUp(t)
	tests := []struct {
		desc    string
		in      string
		want    *metallbv1.L2Advertisement
		wantErr string
	}{{
		desc:    "failure",
		in:      "doesnotexist",
		wantErr: `"doesnotexist" not found`,
	}, {
		desc: "success 1",
		in:   "l2Obj1",
		want: l2Obj1,
	}, {
		desc: "success 2",
		in:   "l2Obj2",
		want: l2Obj2,
	}}
	for _, tt := range tests {
		t.Run(tt.desc, func(t *testing.T) {
			tc := cs.L2Advertisement("test")
			got, err := tc.Get(context.Background(), tt.in, metav1.GetOptions{})
			if s := errdiff.Substring(err, tt.wantErr); s != "" {
				t.Fatalf("unexpected error: %s", s)
			}
			if tt.wantErr != "" {
				return
			}
			if s := cmp.Diff(tt.want, got); s != "" {
				t.Fatalf("Get(%q) failed: %s", tt.in, s)
			}
		})
	}
}

func TestL2AdvertisementDelete(t *testing.T) {
	cs := setUp(t)
	tests := []struct {
		desc    string
		in      string
		wantErr string
	}{{
		desc:    "failure",
		in:      "doesnotexist",
		wantErr: `"doesnotexist" not found`,
	}, {
		desc: "success",
		in:   "l2Obj1",
	}}
	for _, tt := range tests {
		t.Run(tt.desc, func(t *testing.T) {
			tc := cs.L2Advertisement("test")
			err := tc.Delete(context.Background(), tt.in, metav1.DeleteOptions{})
			if s := errdiff.Substring(err, tt.wantErr); s != "" {
				t.Fatalf("unexpected error: %s", s)
			}
			if tt.wantErr != "" {
				return
			}
		})
	}
}

func TestL2AdvertisementWatch(t *testing.T) {
	cs := setUp(t)
	tests := []struct {
		desc    string
		ver     string
		want    watch.Event
		wantErr string
	}{{
		desc:    "failure",
		ver:     "doesnotexist",
		wantErr: "cannot watch unknown resource version",
	}, {
		desc: "success",
		want: watch.Event{
			Type:   watch.Added,
			Object: l2Obj1,
		},
	}}
	for _, tt := range tests {
		t.Run(tt.desc, func(t *testing.T) {
			tc := cs.L2Advertisement("test")
			w, err := tc.Watch(context.Background(), metav1.ListOptions{ResourceVersion: tt.ver})
			if s := errdiff.Substring(err, tt.wantErr); s != "" {
				t.Fatalf("unexpected error: %s", s)
			}
			if tt.wantErr != "" {
				return
			}
			e := <-w.ResultChan()
			if s := cmp.Diff(tt.want, e); s != "" {
				t.Fatalf("Watch() failed: %s", s)
			}
		})
	}
}

func TestL2AdvertisementUpdate(t *testing.T) {
	cs := setUp(t)
	tests := []struct {
		desc    string
		want    *metallbv1.L2Advertisement
		wantErr string
	}{{
		desc: "Error",
		want: &metallbv1.L2Advertisement{
			TypeMeta: metav1.TypeMeta{
				Kind:       "L2Advertisement",
				APIVersion: "metallb.io/v1beta1",
			},
			ObjectMeta: metav1.ObjectMeta{
				Name:      "doesnotexist",
				Namespace: "test",
			},
		},
		wantErr: "doesnotexist",
	}, {
		desc: "Valid L2Advertisement",
		want: l2Obj1,
	}}
	for _, tt := range tests {
		t.Run(tt.desc, func(t *testing.T) {
			tc := cs.L2Advertisement("test")
			updateObj := tt.want.DeepCopy()
			updateObj.Spec.IPAddressPools = append(updateObj.Spec.IPAddressPools, "test-pool2")
			update, err := runtime.DefaultUnstructuredConverter.ToUnstructured(updateObj)
			if err != nil {
				t.Fatalf("failed to generate update: %v", err)
			}
			got, err := tc.Update(context.Background(), &unstructured.Unstructured{Object: update}, metav1.UpdateOptions{})
			if s := errdiff.Substring(err, tt.wantErr); s != "" {
				t.Fatalf("unexpected error: %s", s)
			}
			if tt.wantErr != "" {
				return
			}
			if s := cmp.Diff(updateObj, got); s != "" {
				t.Fatalf("Update() failed: %s", s)
			}
		})
	}
}

func TestL2AdvertisementUnstructured(t *testing.T) {
	cs := setUp(t)
	tests := []struct {
		desc    string
		in      string
		want    *metallbv1.L2Advertisement
		wantErr string
	}{{
		desc:    "failure",
		in:      "missingObj",
		wantErr: `"missingObj" not found`,
	}, {
		desc: "success 1",
		in:   "l2Obj1",
		want: l2Obj1,
	}, {
		desc: "success 2",
		in:   "l2Obj2",
		want: l2Obj2,
	}}
	for _, tt := range tests {
		t.Run(tt.desc, func(t *testing.T) {
			tc := cs.L2Advertisement("test")
			got, err := tc.Unstructured(context.Background(), tt.in, metav1.GetOptions{})
			if s := errdiff.Substring(err, tt.wantErr); s != "" {
				t.Fatalf("unexpected error: %s", s)
			}
			if tt.wantErr != "" {
				return
			}
			uObj1 := &metallbv1.L2Advertisement{}
			if err := runtime.DefaultUnstructuredConverter.FromUnstructured(got.Object, uObj1); err != nil {
				t.Fatalf("failed to turn reponse into a topology: %v", err)
			}
			if s := cmp.Diff(uObj1, tt.want); s != "" {
				t.Fatalf("Unstructured(%q) failed: %s", tt.in, s)
			}
		})
	}
}
