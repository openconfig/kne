// Copyright 2021 Google LLC
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//	http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.
package deploy

import (
	"os"
	"strings"
	"testing"

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
    manifest: ../../manifests/metallb/manifest.yaml
    ip_count: 100
cni:
  kind: Meshnet
  spec:
    manifest: ../../manifests/meshnet/grpc/manifest.yaml
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
    manifest: ../../manifests/metallb/manifest.yaml
    ip_count: 100
cni:
  kind: Meshnet
  spec:
    manifest: ../../manifests/meshnet/grpc/manifest.yaml
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
    manifest: ../../manifests/metallb/manifest.yaml
    ip_count: 100
cni:
  kind: InvalidCNI
  spec:
    manifest: ../../manifests/meshnet/grpc/manifest.yaml
`
	invalidControllers = `
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
    manifest: ../../manifests/metallb/manifest.yaml
    ip_count: 100
cni:
  kind: Meshnet
  spec:
    manifest: ../../manifests/meshnet/grpc/manifest.yaml
controllers:
  - kind: InvalidController
    spec:
      operator: path/to/manifest.yaml
`
	validControllers = `
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
    manifest: ../../manifests/metallb/manifest.yaml
    ip_count: 100
cni:
  kind: Meshnet
  spec:
    manifest: ../../manifests/meshnet/grpc/manifest.yaml
controllers:
  - kind: IxiaTG
    spec:
      operator: path/to/manifest.yaml
      configMap: path/to/configmap.yaml
  - kind: SRLinux
    spec:
      operator: path/to/manifest.yaml
  - kind: CEOSLab
    spec:
      operator: path/to/manifest.yaml
  - kind: Lemming
    spec:
      operator: path/to/manifest.yaml
`
)

func TestNew(t *testing.T) {
	c := New()
	if !strings.HasPrefix(c.Use, "deploy") {
		t.Fatalf("unexpected command object: got %q, want \"deploy\"", c.Use)
	}
}

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
		desc:    "invalid controllers",
		cfg:     invalidControllers,
		wantErr: "controller type not supported",
	}, {
		desc: "valid controllers",
		cfg:  validControllers,
	}, {
		desc: "kind example",
		cfg:  "",
		path: "testdata/kind-deployment.yaml",
	}}
	for _, tt := range tests {
		t.Run(tt.desc, func(t *testing.T) {
			if tt.path == "" {
				f, err := os.CreateTemp("", "dtest")
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
			d, err := newDeployment(tt.path)
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
