package main

import (
	"testing"

	"github.com/google/go-cmp/cmp"
	"github.com/google/go-cmp/cmp/cmpopts"
	"github.com/google/kne/deploy"
	cpb "github.com/google/kne/proto/controller"
	"github.com/h-fam/errdiff"
)

func TestNewDeployment(t *testing.T) {
	d := defaultMetallbManifestDir
	defer func() {
		defaultMetallbManifestDir = d
	}()
	d = defaultMeshnetManifestDir
	defer func() {
		defaultMeshnetManifestDir = d
	}()

	tests := []struct {
		desc                         string
		req                          *cpb.CreateClusterRequest
		defaultMeshnetManifestDirDNE bool
		defaultMetallbManifestDirDNE bool
		want                         *deploy.Deployment
		wantErr                      string
	}{{
		desc: "full request spec",
		req: &cpb.CreateClusterRequest{
			ClusterSpec: &cpb.CreateClusterRequest_Kind{
				&cpb.KindSpec{
					Name:    "kne",
					Recycle: true,
					Version: "0.11.1",
					Image:   "kindest/node:v1.22.1",
				},
			},
			IngressSpec: &cpb.CreateClusterRequest_Metallb{
				&cpb.MetallbSpec{
					ManifestDir: "/home",
					IpCount:     100,
				},
			},
			CniSpec: &cpb.CreateClusterRequest_Meshnet{
				&cpb.MeshnetSpec{
					ManifestDir: "/home",
				},
			},
		},
		want: &deploy.Deployment{
			Cluster: &deploy.KindSpec{
				Name:    "kne",
				Recycle: true,
				Version: "0.11.1",
				Image:   "kindest/node:v1.22.1",
			},
			Ingress: &deploy.MetalLBSpec{
				ManifestDir: "/home",
				IPCount:     100,
			},
			CNI: &deploy.MeshnetSpec{
				ManifestDir: "/home",
			},
		},
	}, {
		desc: "full request spec - default manifest paths",
		req: &cpb.CreateClusterRequest{
			ClusterSpec: &cpb.CreateClusterRequest_Kind{
				&cpb.KindSpec{
					Name:    "kne",
					Recycle: true,
					Version: "0.11.1",
					Image:   "kindest/node:v1.22.1",
				},
			},
			IngressSpec: &cpb.CreateClusterRequest_Metallb{
				&cpb.MetallbSpec{
					IpCount: 100,
				},
			},
			CniSpec: &cpb.CreateClusterRequest_Meshnet{
				&cpb.MeshnetSpec{},
			},
		},
		want: &deploy.Deployment{
			Cluster: &deploy.KindSpec{
				Name:    "kne",
				Recycle: true,
				Version: "0.11.1",
				Image:   "kindest/node:v1.22.1",
			},
			Ingress: &deploy.MetalLBSpec{
				ManifestDir: "/",
				IPCount:     100,
			},
			CNI: &deploy.MeshnetSpec{
				ManifestDir: "/",
			},
		},
	}, {
		desc: "full request spec - default manifest paths dne",
		req: &cpb.CreateClusterRequest{
			ClusterSpec: &cpb.CreateClusterRequest_Kind{
				&cpb.KindSpec{
					Name:    "kne",
					Recycle: true,
					Version: "0.11.1",
					Image:   "kindest/node:v1.22.1",
				},
			},
			IngressSpec: &cpb.CreateClusterRequest_Metallb{
				&cpb.MetallbSpec{
					IpCount: 100,
				},
			},
			CniSpec: &cpb.CreateClusterRequest_Meshnet{
				&cpb.MeshnetSpec{},
			},
		},
		defaultMeshnetManifestDirDNE: true,
		defaultMetallbManifestDirDNE: true,
		wantErr:                      "failed to validate path",
	}, {
		desc: "bad metallb manifest dir",
		req: &cpb.CreateClusterRequest{
			ClusterSpec: &cpb.CreateClusterRequest_Kind{
				&cpb.KindSpec{
					Name:    "kne",
					Recycle: true,
					Version: "0.11.1",
					Image:   "kindest/node:v1.22.1",
				},
			},
			IngressSpec: &cpb.CreateClusterRequest_Metallb{
				&cpb.MetallbSpec{
					ManifestDir: "/foo",
					IpCount:     100,
				},
			},
			CniSpec: &cpb.CreateClusterRequest_Meshnet{
				&cpb.MeshnetSpec{},
			},
		},
		wantErr: `failed to validate path "/foo"`,
	}, {
		desc:    "empty kind spec",
		req:     &cpb.CreateClusterRequest{},
		wantErr: "cluster type not supported",
	}, {
		desc: "empty ingress spec",
		req: &cpb.CreateClusterRequest{
			ClusterSpec: &cpb.CreateClusterRequest_Kind{
				&cpb.KindSpec{
					Name:    "kne",
					Recycle: true,
					Version: "0.11.1",
					Image:   "kindest/node:v1.22.1",
				},
			},
			CniSpec: &cpb.CreateClusterRequest_Meshnet{
				&cpb.MeshnetSpec{
					ManifestDir: "../../bar",
				},
			},
		},
		wantErr: "ingress spec not supported",
	}, {
		desc: "bad meshnet manifest dir",
		req: &cpb.CreateClusterRequest{
			ClusterSpec: &cpb.CreateClusterRequest_Kind{
				&cpb.KindSpec{
					Name:    "kne",
					Recycle: true,
					Version: "0.11.1",
					Image:   "kindest/node:v1.22.1",
				},
			},
			IngressSpec: &cpb.CreateClusterRequest_Metallb{
				&cpb.MetallbSpec{
					IpCount: 100,
				},
			},
			CniSpec: &cpb.CreateClusterRequest_Meshnet{
				&cpb.MeshnetSpec{
					ManifestDir: "/foo",
				},
			},
		},
		wantErr: "failed to validate path",
	}, {
		desc: "cni spec empty",
		req: &cpb.CreateClusterRequest{
			ClusterSpec: &cpb.CreateClusterRequest_Kind{
				&cpb.KindSpec{
					Name:    "kne",
					Recycle: true,
					Version: "0.11.1",
					Image:   "kindest/node:v1.22.1",
				},
			},
			IngressSpec: &cpb.CreateClusterRequest_Metallb{
				&cpb.MetallbSpec{
					ManifestDir: "/usr/local",
					IpCount:     100,
				},
			},
		},
		wantErr: "cni type not supported",
	}}
	for _, tt := range tests {
		t.Run(tt.desc, func(t *testing.T) {
			defaultMetallbManifestDir = "/"
			if tt.defaultMetallbManifestDirDNE {
				defaultMetallbManifestDir = "/this/path/dne"
			}
			defaultMeshnetManifestDir = "/"
			if tt.defaultMeshnetManifestDirDNE {
				defaultMeshnetManifestDir = "/this/path/dne"
			}
			got, err := newDeployment(tt.req)
			if s := errdiff.Substring(err, tt.wantErr); s != "" {
				t.Fatalf("newDeployment() unexpected error: %s", s)
			}
			if s := cmp.Diff(tt.want, got, cmpopts.IgnoreUnexported(deploy.KindSpec{}, deploy.MeshnetSpec{}, deploy.MetalLBSpec{})); s != "" {
				t.Errorf("newDeployment() unexpected diff (-want +got):\n%s", s)
			}
		})
	}

}
