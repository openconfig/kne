package v1beta1

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"reflect"
	"testing"

	"github.com/h-fam/errdiff"
	"github.com/kr/pretty"
	topologyv1 "github.com/openconfig/kne/api/types/v1beta1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/watch"
	dynamicfake "k8s.io/client-go/dynamic/fake"
	"k8s.io/client-go/kubernetes/scheme"
	"k8s.io/client-go/rest"
	restfake "k8s.io/client-go/rest/fake"
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

func setUp(t *testing.T) (*Clientset, *restfake.RESTClient) {
	t.Helper()
	fakeClient := &restfake.RESTClient{
		NegotiatedSerializer: scheme.Codecs.WithoutConversion(),
		GroupVersion:         *groupVersion,
		VersionedAPIPath:     topologyv1.GroupVersion,
	}
	cs, err := NewForConfig(&rest.Config{})
	if err != nil {
		t.Fatalf("NewForConfig() failed: %v", err)
	}
	objs := []runtime.Object{obj1, obj2}
	cs.restClient = fakeClient
	f := dynamicfake.NewSimpleDynamicClient(scheme.Scheme, objs...)
	// Add handler for Update call.
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
	cs.dInterface = f.Resource(gvr)
	return cs, fakeClient
}

func TestCreate(t *testing.T) {
	cs, fakeClient := setUp(t)
	tests := []struct {
		desc    string
		resp    *http.Response
		want    *topologyv1.Topology
		wantErr string
	}{{
		desc:    "Error",
		wantErr: "TEST ERROR",
	}, {
		desc: "Valid Topology",
		resp: &http.Response{
			StatusCode: http.StatusOK,
		},
		want: obj1,
	}}
	for _, tt := range tests {
		fakeClient.Err = nil
		if tt.wantErr != "" {
			fakeClient.Err = fmt.Errorf(tt.wantErr)
		}
		fakeClient.Resp = tt.resp
		if tt.want != nil {
			b, _ := json.Marshal(tt.want)
			tt.resp.Body = io.NopCloser(bytes.NewReader(b))
		}
		t.Run(tt.desc, func(t *testing.T) {
			tc := cs.Topology("foo")
			got, err := tc.Create(context.Background(), tt.want, metav1.CreateOptions{})
			if s := errdiff.Substring(err, tt.wantErr); s != "" {
				t.Fatalf("unexpected error: %s", s)
			}
			if tt.wantErr != "" {
				return
			}
			want := tt.want.DeepCopy()
			want.TypeMeta = metav1.TypeMeta{}
			if !reflect.DeepEqual(got, want) {
				t.Fatalf("Create(%+v) failed: diff\n%s", tt.want, pretty.Diff(got, want))
			}
		})
	}
}

func TestList(t *testing.T) {
	cs, fakeClient := setUp(t)
	tests := []struct {
		desc    string
		resp    *http.Response
		want    *topologyv1.TopologyList
		wantErr string
	}{{
		desc:    "Error",
		wantErr: "TEST ERROR",
	}, {
		desc: "Valid Topology",
		resp: &http.Response{
			StatusCode: http.StatusOK,
		},
		want: &topologyv1.TopologyList{
			Items: []topologyv1.Topology{*obj1, *obj2},
		},
	}}
	for _, tt := range tests {
		fakeClient.Err = nil
		if tt.wantErr != "" {
			fakeClient.Err = fmt.Errorf(tt.wantErr)
		}
		fakeClient.Resp = tt.resp
		if tt.want != nil {
			b, _ := json.Marshal(tt.want)
			tt.resp.Body = io.NopCloser(bytes.NewReader(b))
		}
		t.Run(tt.desc, func(t *testing.T) {
			tc := cs.Topology("foo")
			got, err := tc.List(context.Background(), metav1.ListOptions{})
			if s := errdiff.Substring(err, tt.wantErr); s != "" {
				t.Fatalf("unexpected error: %s", s)
			}
			if tt.wantErr != "" {
				return
			}
			if !reflect.DeepEqual(got, tt.want) {
				t.Fatalf("List() failed: got %v, want %v", got, tt.want)
			}
		})
	}
}

func TestGet(t *testing.T) {
	cs, fakeClient := setUp(t)
	tests := []struct {
		desc    string
		resp    *http.Response
		want    *topologyv1.Topology
		wantErr string
	}{{
		desc:    "Error",
		wantErr: "TEST ERROR",
	}, {
		desc: "Valid Topology",
		resp: &http.Response{
			StatusCode: http.StatusOK,
		},
		want: obj1,
	}}
	for _, tt := range tests {
		fakeClient.Err = nil
		if tt.wantErr != "" {
			fakeClient.Err = fmt.Errorf(tt.wantErr)
		}
		fakeClient.Resp = tt.resp
		if tt.want != nil {
			b, _ := json.Marshal(tt.want)
			tt.resp.Body = io.NopCloser(bytes.NewReader(b))
		}
		t.Run(tt.desc, func(t *testing.T) {
			tc := cs.Topology("foo")
			got, err := tc.Get(context.Background(), "test", metav1.GetOptions{})
			if s := errdiff.Substring(err, tt.wantErr); s != "" {
				t.Fatalf("unexpected error: %s", s)
			}
			if tt.wantErr != "" {
				return
			}
			want := tt.want.DeepCopy()
			want.TypeMeta = metav1.TypeMeta{}
			if !reflect.DeepEqual(got, want) {
				t.Fatalf("Get() failed: got %v, want %v", got, want)
			}
		})
	}
}

func TestDelete(t *testing.T) {
	cs, fakeClient := setUp(t)
	tests := []struct {
		desc    string
		resp    *http.Response
		wantErr string
	}{{
		desc:    "Error",
		wantErr: "TEST ERROR",
	}, {
		desc: "Valid Topology",
		resp: &http.Response{
			StatusCode: http.StatusOK,
		},
	}}
	for _, tt := range tests {
		fakeClient.Err = nil
		if tt.wantErr != "" {
			fakeClient.Err = fmt.Errorf(tt.wantErr)
		}
		fakeClient.Resp = tt.resp
		t.Run(tt.desc, func(t *testing.T) {
			tc := cs.Topology("foo")
			err := tc.Delete(context.Background(), "obj1", metav1.DeleteOptions{})
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
	cs, fakeClient := setUp(t)
	tests := []struct {
		desc    string
		resp    *http.Response
		want    *watch.Event
		wantErr string
	}{{
		desc:    "Error",
		wantErr: "TEST ERROR",
	}}
	for _, tt := range tests {
		fakeClient.Err = nil
		if tt.wantErr != "" {
			fakeClient.Err = fmt.Errorf(tt.wantErr)
		}
		fakeClient.Resp = tt.resp
		if tt.want != nil {
			b, _ := json.Marshal(tt.want)
			tt.resp.Body = io.NopCloser(bytes.NewReader(b))
		}
		t.Run(tt.desc, func(t *testing.T) {
			tc := cs.Topology("foo")
			w, err := tc.Watch(context.Background(), metav1.ListOptions{})
			if s := errdiff.Substring(err, tt.wantErr); s != "" {
				t.Fatalf("unexpected error: %s", s)
			}
			if tt.wantErr != "" {
				return
			}
			e := <-w.ResultChan()
			if !reflect.DeepEqual(e, tt.want) {
				t.Fatalf("Watch() failed: got %v, want %v", e, tt.want)
			}
		})
	}
}

func TestUpdate(t *testing.T) {
	cs, _ := setUp(t)
	tests := []struct {
		desc    string
		resp    *http.Response
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
			want := tt.want.DeepCopy()
			want.TypeMeta = metav1.TypeMeta{}
			if !reflect.DeepEqual(got, updateObj) {
				t.Fatalf("Update() failed: got %+v, want %+v", got, want)
			}
		})
	}
}

func TestUnstructured(t *testing.T) {
	cs, _ := setUp(t)
	tests := []struct {
		desc    string
		in      string
		want    *topologyv1.Topology
		wantErr string
	}{{
		desc:    "Error",
		in:      "missingObj",
		wantErr: `"missingObj" not found`,
	}, {
		desc: "Valid Topology",
		in:   "obj1",
		want: obj1,
	}, {
		desc: "Valid Topology",
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
			if !reflect.DeepEqual(uObj1, tt.want) {
				t.Fatalf("Unstructured(%q) failed: got %+v, want %+v", tt.in, uObj1, tt.want)
			}
		})
	}
}
