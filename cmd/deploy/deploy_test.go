package deploy

import (
	"context"
	"fmt"
	"io/ioutil"
	"os"
	"testing"

	"github.com/golang/mock/gomock"
	"github.com/google/kne/cmd/deploy/mocks"
	"github.com/h-fam/errdiff"
)

var (
	invalidCluster = `
cluster:
  kind: InvalidCluster
  spec: 
    name: kne
    recycle: True
    version: 0.11.1
    image: kindest/node:v1.22.0
ingress:
  kind: MetalLB
  spec:
    manifests: ../../manifests/metallb
    ip_count: 100
cni:
  kind: Meshnet
  spec:
    manifests: ../../manifests/meshnet/base
`
	invalidIngress = `
cluster:
  kind: Kind
  spec: 
    name: kne
    recycle: True
    version: 0.11.1
    image: kindest/node:v1.22.0
ingress:
  kind: InvalidIngress
  spec:
    manifests: ../../manifests/metallb
    ip_count: 100
cni:
  kind: Meshnet
  spec:
    manifests: ../../manifests/meshnet/base
`
	invalidCNI = `
cluster:
  kind: Kind
  spec: 
    name: kne
    recycle: True
    version: 0.11.1
    image: kindest/node:v1.22.0
ingress:
  kind: MetalLB
  spec:
    manifests: ../../manifests/metallb
    ip_count: 100
cni:
  kind: InvalidCNI
  spec:
    manifests: ../../manifests/meshnet/base`
)

func TestNewDeployment(t *testing.T) {
	tests := []struct {
		desc    string
		cfg     string
		path    string
		wantErr string
	}{{
		desc:    "invalid cluster",
		cfg:     invalidCluster,
		wantErr: "cluster type not supported",
	}, {
		desc:    "invalid ingress",
		cfg:     invalidIngress,
		wantErr: "ingress type not supported",
	}, {
		desc:    "invalid cni",
		cfg:     invalidCNI,
		wantErr: "CNI type not supported",
	}, {
		desc: "kind example",
		cfg:  "",
		path: "../../deploy/kne/kind.yaml",
	}}
	for _, tt := range tests {
		t.Run(tt.desc, func(t *testing.T) {
			if tt.path == "" {
				f, err := ioutil.TempFile("", "dtest")
				if err != nil {
					t.Fatalf("failed to create tempfile: %v", err)
				}
				if _, err := f.Write([]byte(tt.cfg)); err != nil {
					t.Fatalf("failed to create tempfile: %v", err)
				}
				if err := f.Close(); err != nil {
					t.Fatalf("failed to create tempfile: %v", err)
				}
				tt.path = f.Name()
				defer os.Remove(f.Name())
			}
			cfg, _, err := deploymentFromArg(tt.path)
			if err != nil {
				t.Fatalf("failed to unmarshal config: %v", err)
			}
			d, err := NewDeployment(cfg)
			if s := errdiff.Substring(err, tt.wantErr); s != "" {
				t.Fatalf("unexpected error: %s", s)
			}
			if err != nil {
				return
			}
			t.Log(d)
		})
	}
}

func TestKindSpec(t *testing.T) {
	mockCtrl := gomock.NewController(t)
	defer mockCtrl.Finish()
	tests := []struct {
		desc        string
		k           *KindSpec
		wantErr     string
		mockExpects func(m *mocks.Mockprovider)
	}{{
		desc: "create cluster",
		k: &KindSpec{
			Name:    "testCluster",
			Recycle: false,
		},
		mockExpects: func(m *mocks.Mockprovider) {
			m.EXPECT().Create("testCluster", gomock.Any()).Return(nil)
		},
	}, {
		desc: "create cluster with recycle",
		k: &KindSpec{
			Name:    "testCluster",
			Recycle: true,
		},
		mockExpects: func(m *mocks.Mockprovider) {
			m.EXPECT().Create("testCluster", gomock.Any()).Return(nil)
			m.EXPECT().List().Return([]string{"test1"}, nil)
		},
	}, {
		desc: "exists cluster with recycle",
		k: &KindSpec{
			Name:    "testCluster",
			Recycle: true,
		},
		mockExpects: func(m *mocks.Mockprovider) {
			m.EXPECT().List().Return([]string{"testCluster"}, nil)
		},
	}, {
		desc: "create fail",
		k: &KindSpec{
			Name:    "testCluster",
			Recycle: false,
		},
		mockExpects: func(m *mocks.Mockprovider) {
			m.EXPECT().Create("testCluster", gomock.Any()).Return(fmt.Errorf("create failed"))
		},
		wantErr: "failed to create cluster",
	}}
	for _, tt := range tests {
		t.Run(tt.desc, func(t *testing.T) {
			m := mocks.NewMockprovider(mockCtrl)
			tt.mockExpects(m)
			newProvider = func() provider {
				return m
			}
			err := tt.k.Deploy(context.Background())
			if s := errdiff.Substring(err, tt.wantErr); s != "" {
				t.Fatalf("unexpected error: %s", s)
			}
		})
	}
}
