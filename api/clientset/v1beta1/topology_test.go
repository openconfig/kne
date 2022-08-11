package v1beta1

import (
	"context"
	"fmt"
	"testing"

	"github.com/google/go-cmp/cmp"
	"github.com/google/go-cmp/cmp/cmpopts"
	"github.com/h-fam/errdiff"
	topologyv1 "github.com/openconfig/kne/api/types/v1beta1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/watch"
	dynamicfake "k8s.io/client-go/dynamic/fake"
	"k8s.io/client-go/kubernetes/scheme"
	"k8s.io/client-go/rest"
	ktest "k8s.io/client-go/testing"
)

var (
	obj1 = &topologyv1.Topology{
		TypeMeta: metav1.TypeMeta{
			Kind:       "Topology",
			APIVersion: "networkop.co.uk/v1beta1",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:       "obj1",
			Namespace:  "test",
			Generation: 1,
		},
		Status: topologyv1.TopologyStatus{},
		Spec: topologyv1.TopologySpec{
			Links: []topologyv1.Link{{
				LocalIntf: "int1",
				PeerIntf:  "int1",
				PeerPod:   "obj2",
				UID:       0,
			}},
		},
	}
	obj2 = &topologyv1.Topology{
		TypeMeta: metav1.TypeMeta{
			Kind:       "Topology",
			APIVersion: "networkop.co.uk/v1beta1",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:       "obj2",
			Namespace:  "test",
			Generation: 1,
		},
		Status: topologyv1.TopologyStatus{},
		Spec: topologyv1.TopologySpec{
			Links: []topologyv1.Link{{
				LocalIntf: "int1",
				PeerIntf:  "int1",
				PeerPod:   "obj1",
				UID:       1,
			}},
		},
	}
)

type fakeWatch struct {
	e    []watch.Event
	ch   chan watch.Event
	done chan struct{}
}

func newFakeWatch(e []watch.Event) *fakeWatch {
	f := &fakeWatch{
		e:    e,
		ch:   make(chan watch.Event, 1),
		done: make(chan struct{}),
	}
	go func() {
		for len(f.e) != 0 {
			e := f.e[0]
			f.e = f.e[1:]
			select {
			case f.ch <- e:
			case <-f.done:
				return
			}
		}
	}()
	return f
}
func (f *fakeWatch) Stop() {
	close(f.done)
}

func (f *fakeWatch) ResultChan() <-chan watch.Event {
	return f.ch
}

func setUp(t *testing.T) *Clientset {
	t.Helper()
	cs, err := NewForConfig(&rest.Config{})
	if err != nil {
		t.Fatalf("NewForConfig() failed: %v", err)
	}
	objs := []runtime.Object{obj1, obj2}
	f := dynamicfake.NewSimpleDynamicClient(scheme.Scheme, objs...)
	f.PrependReactor("create", "*", func(action ktest.Action) (bool, runtime.Object, error) {
		cAction, ok := action.(ktest.CreateAction)
		if !ok {
			return false, nil, nil
		}
		uObj := cAction.GetObject().(*unstructured.Unstructured)
		tObj := &topologyv1.Topology{}
		if err := runtime.DefaultUnstructuredConverter.FromUnstructured(uObj.Object, tObj); err != nil {
			return true, nil, fmt.Errorf("failed to covert object: %v", err)
		}
		if tObj.ObjectMeta.Name == "alreadyexists" {
			return true, nil, fmt.Errorf("alreadyexists")
		}
		return true, tObj, nil
	})
	f.PrependReactor("update", "*", func(action ktest.Action) (bool, runtime.Object, error) {
		uAction, ok := action.(ktest.UpdateAction)
		if !ok {
			return false, nil, nil
		}
		uObj := uAction.GetObject().(*unstructured.Unstructured)
		tObj := &topologyv1.Topology{}
		if err := runtime.DefaultUnstructuredConverter.FromUnstructured(uObj.Object, tObj); err != nil {
			return true, nil, fmt.Errorf("failed to covert object: %v", err)
		}
		if tObj.ObjectMeta.Name == "doesnotexist" {
			return true, nil, fmt.Errorf("doesnotexist")
		}
		return true, uAction.GetObject(), nil
	})
	f.PrependWatchReactor("*", func(action ktest.Action) (bool, watch.Interface, error) {
		wAction, ok := action.(ktest.WatchAction)
		if !ok {
			return false, nil, nil
		}
		if wAction.GetWatchRestrictions().ResourceVersion == "doesnotexist" {
			return true, nil, fmt.Errorf("cannot watch unknown resource version")
		}
		f := newFakeWatch([]watch.Event{
			{
				Type:   watch.Added,
				Object: obj1,
			},
		})
		return true, f, nil
	})
	cs.dInterface = f.Resource(gvr)
	return cs
}

func TestCreate(t *testing.T) {
	cs := setUp(t)
	tests := []struct {
		desc    string
		want    *topologyv1.Topology
		wantErr string
	}{{
		desc: "already exists",
		want: &topologyv1.Topology{
			TypeMeta: metav1.TypeMeta{
				Kind:       "Topology",
				APIVersion: "networkop.co.uk/v1beta1",
			},
			ObjectMeta: metav1.ObjectMeta{
				Name:      "alreadyexists",
				Namespace: "test",
			},
		},
		wantErr: "alreadyexists",
	}, {
		desc: "success",
		want: obj1,
	}}
	for _, tt := range tests {
		t.Run(tt.desc, func(t *testing.T) {
			tc := cs.Topology("test")
			got, err := tc.Create(context.Background(), tt.want, metav1.CreateOptions{})
			if s := errdiff.Substring(err, tt.wantErr); s != "" {
				t.Fatalf("unexpected error: %s", s)
			}
			if tt.wantErr != "" {
				return
			}
			if s := cmp.Diff(got, tt.want, cmpopts.IgnoreFields(topologyv1.Topology{}, "TypeMeta")); s != "" {
				t.Fatalf("Create(%+v) failed: %s", tt.want, s)
			}
		})
	}
}

func TestList(t *testing.T) {
	cs := setUp(t)
	tests := []struct {
		desc    string
		want    *topologyv1.TopologyList
		wantErr string
	}{{
		desc: "success",
		want: &topologyv1.TopologyList{
			Items: []topologyv1.Topology{*obj1, *obj2},
		},
	}}
	for _, tt := range tests {
		t.Run(tt.desc, func(t *testing.T) {
			tc := cs.Topology("test")
			got, err := tc.List(context.Background(), metav1.ListOptions{})
			if s := errdiff.Substring(err, tt.wantErr); s != "" {
				t.Fatalf("unexpected error: %s", s)
			}
			if tt.wantErr != "" {
				return
			}
			if s := cmp.Diff(got, tt.want, cmpopts.IgnoreFields(topologyv1.TopologyList{}, "TypeMeta")); s != "" {
				t.Fatalf("List() failed: %s", s)
			}
		})
	}
}

func TestGet(t *testing.T) {
	cs := setUp(t)
	tests := []struct {
		desc    string
		in      string
		want    *topologyv1.Topology
		wantErr string
	}{{
		desc:    "failure",
		in:      "doesnotexist",
		wantErr: `"doesnotexist" not found`,
	}, {
		desc: "success 1",
		in:   "obj1",
		want: obj1,
	}, {
		desc: "success 2",
		in:   "obj2",
		want: obj2,
	}}
	for _, tt := range tests {
		t.Run(tt.desc, func(t *testing.T) {
			tc := cs.Topology("test")
			got, err := tc.Get(context.Background(), tt.in, metav1.GetOptions{})
			if s := errdiff.Substring(err, tt.wantErr); s != "" {
				t.Fatalf("unexpected error: %s", s)
			}
			if tt.wantErr != "" {
				return
			}
			if s := cmp.Diff(got, tt.want); s != "" {
				t.Fatalf("Get(%q) failed: %s", tt.in, s)
			}
		})
	}
}

func TestDelete(t *testing.T) {
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
		in:   "obj1",
	}}
	for _, tt := range tests {
		t.Run(tt.desc, func(t *testing.T) {
			tc := cs.Topology("test")
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

func TestWatch(t *testing.T) {
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
			Object: obj1,
		},
	}}
	for _, tt := range tests {
		t.Run(tt.desc, func(t *testing.T) {
			tc := cs.Topology("test")
			w, err := tc.Watch(context.Background(), metav1.ListOptions{ResourceVersion: tt.ver})
			if s := errdiff.Substring(err, tt.wantErr); s != "" {
				t.Fatalf("unexpected error: %s", s)
			}
			if tt.wantErr != "" {
				return
			}
			e := <-w.ResultChan()
			if s := cmp.Diff(e, tt.want); s != "" {
				t.Fatalf("Watch() failed: %s", s)
			}
		})
	}
}

func TestUpdate(t *testing.T) {
	cs := setUp(t)
	tests := []struct {
		desc    string
		want    *topologyv1.Topology
		wantErr string
	}{{
		desc: "Error",
		want: &topologyv1.Topology{
			TypeMeta: metav1.TypeMeta{
				Kind:       "Topology",
				APIVersion: "networkop.co.uk/v1beta1",
			},
			ObjectMeta: metav1.ObjectMeta{
				Name:      "doesnotexist",
				Namespace: "test",
			},
		},
		wantErr: "doesnotexist",
	}, {
		desc: "Valid Topology",
		want: obj1,
	}}
	for _, tt := range tests {
		t.Run(tt.desc, func(t *testing.T) {
			tc := cs.Topology("test")
			updateObj := tt.want.DeepCopy()
			updateObj.Spec.Links = append(updateObj.Spec.Links, topologyv1.Link{UID: 1000})
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
			if s := cmp.Diff(got, updateObj); s != "" {
				t.Fatalf("Update() failed: %s", s)
			}
		})
	}
}

func TestUnstructured(t *testing.T) {
	cs := setUp(t)
	tests := []struct {
		desc    string
		in      string
		want    *topologyv1.Topology
		wantErr string
	}{{
		desc:    "failure",
		in:      "missingObj",
		wantErr: `"missingObj" not found`,
	}, {
		desc: "success 1",
		in:   "obj1",
		want: obj1,
	}, {
		desc: "success 2",
		in:   "obj2",
		want: obj2,
	}}
	for _, tt := range tests {
		t.Run(tt.desc, func(t *testing.T) {
			tc := cs.Topology("test")
			got, err := tc.Unstructured(context.Background(), tt.in, metav1.GetOptions{})
			if s := errdiff.Substring(err, tt.wantErr); s != "" {
				t.Fatalf("unexpected error: %s", s)
			}
			if tt.wantErr != "" {
				return
			}
			uObj1 := &topologyv1.Topology{}
			if err := runtime.DefaultUnstructuredConverter.FromUnstructured(got.Object, uObj1); err != nil {
				t.Fatalf("failed to turn reponse into a topology: %v", err)
			}
			if s := cmp.Diff(uObj1, tt.want); s != "" {
				t.Fatalf("Unstructured(%q) failed: %s", tt.in, s)
			}
		})
	}
}
