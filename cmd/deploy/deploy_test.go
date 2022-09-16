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
    manifests: ../../manifests/metallb
    ip_count: 100
cni:
  kind: Meshnet
  spec:
    manifests: ../../manifests/meshnet/base
controllers:
  - kind: InvalidController
    spec:
      manifests: path/to/manifest`
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
    manifests: ../../manifests/metallb
    ip_count: 100
cni:
  kind: Meshnet
  spec:
    manifests: ../../manifests/meshnet/base
controllers:
  - kind: IxiaTG
    spec:
      manifests: path/to/manifest
      configMap:
        release: some-value
        images:
          - name: controller
            path: some/path
            tag: latest
  - kind: SRLinux
    spec:
      manifests: path/to/manifest
  - kind: CEOSLab
    spec:
      manifests: path/to/manifest`
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
		path: "../../deploy/kne/kind.yaml",
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
