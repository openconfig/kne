package v1beta1

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io/ioutil"
	"net/http"
	"reflect"
	"testing"

	topologyv1 "github.com/google/kne/api/types/v1beta1"
	"github.com/h-fam/errdiff"
	"k8s.io/apimachinery/pkg/runtime"
	dynamicfake "k8s.io/client-go/dynamic/fake"
	"k8s.io/client-go/kubernetes/scheme"
	"k8s.io/client-go/rest"
	restfake "k8s.io/client-go/rest/fake"
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
	cs.restClient = fakeClient
	cs.dInterface = dynamicfake.NewSimpleDynamicClient(runtime.NewScheme()).Resource(gvr)
	return cs, fakeClient
}

func TestList(t *testing.T) {
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
		want: &topologyv1.Topology{
			Spec: topologyv1.TopologySpec{
				Links: []topologyv1.Link{{
					LocalIntf: "int1",
					PeerIntf:  "int2",
					PeerPod:   "pod2",
					UID:       1,
				}},
			},
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
			tt.resp.Body = ioutil.NopCloser(bytes.NewReader(b))
		}
		t.Run(tt.desc, func(t *testing.T) {
			tc := cs.Topology("foo")
			got, err := tc.Create(context.Background(), tt.want)
			if s := errdiff.Substring(err, tt.wantErr); s != "" {
				t.Fatalf("unexpected error: %s", s)
			}
			if tt.wantErr != "" {
				return
			}
			if !reflect.DeepEqual(got, tt.want) {
				t.Fatalf("Create(%v) failed: got %v, want %v", tt.want, got, tt.want)
			}
		})
	}

}
