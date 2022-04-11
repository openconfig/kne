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
	mlb := defaultMetallbManifestDir
	defer func() {
		defaultMetallbManifestDir = mlb
	}()
	m := defaultMeshnetManifestDir
	defer func() {
		defaultMeshnetManifestDir = m
	}()
	itg := defaultIxiaTGManifestDir
	defer func() {
		defaultIxiaTGManifestDir = itg
	}()

	tests := []struct {
		desc                         string
		req                          *cpb.CreateClusterRequest
		defaultMeshnetManifestDirDNE bool
		defaultMetallbManifestDirDNE bool
		defaultIxiaTGManifestDirDNE  bool
		want                         *deploy.Deployment
		wantErr                      string
	}{{
		desc: "request spec",
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
		desc: "request spec - with ixiatg controller",
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
			ControllerSpecs: []*cpb.ControllerSpec{{
				Spec: &cpb.ControllerSpec_Ixiatg{
					&cpb.IxiaTGSpec{
						ManifestDir: "/home",
					},
				},
			}},
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
			Controllers: []deploy.Controller{
				&deploy.IxiaTGSpec{
					ManifestDir: "/home",
				},
			},
		},
	}, {
		desc: "request spec - with ixiatg controller config map",
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
			ControllerSpecs: []*cpb.ControllerSpec{{
				Spec: &cpb.ControllerSpec_Ixiatg{
					&cpb.IxiaTGSpec{
						ManifestDir: "/home",
						ConfigMap: &cpb.IxiaTGConfigMap{
							Release: "0.0.1-9999",
							Images: []*cpb.IxiaTGImage{{
								Name: "a",
								Path: "a-path",
								Tag:  "a-tag",
							}, {
								Name: "b",
								Path: "b-path",
								Tag:  "b-tag",
							}},
						},
					},
				},
			}},
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
			Controllers: []deploy.Controller{
				&deploy.IxiaTGSpec{
					ManifestDir: "/home",
					ConfigMap: &deploy.IxiaTGConfigMap{
						Release: "0.0.1-9999",
						Images: []*deploy.IxiaTGImage{{
							Name: "a",
							Path: "a-path",
							Tag:  "a-tag",
						}, {
							Name: "b",
							Path: "b-path",
							Tag:  "b-tag",
						}},
					},
				},
			},
		},
	}, {
		desc: "request spec - default manifest paths",
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
			ControllerSpecs: []*cpb.ControllerSpec{{
				Spec: &cpb.ControllerSpec_Ixiatg{
					&cpb.IxiaTGSpec{},
				},
			}},
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
			Controllers: []deploy.Controller{
				&deploy.IxiaTGSpec{
					ManifestDir: "/",
				},
			},
		},
	}, {
		desc: "request spec - default manifest paths dne",
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
			ControllerSpecs: []*cpb.ControllerSpec{{
				Spec: &cpb.ControllerSpec_Ixiatg{
					&cpb.IxiaTGSpec{},
				},
			}},
		},
		defaultMeshnetManifestDirDNE: true,
		defaultMetallbManifestDirDNE: true,
		defaultIxiaTGManifestDirDNE:  true,
		wantErr:                      "failed to validate path",
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
		desc: "empty cni spec",
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
		desc: "bad ixiatg manifest dir",
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
			ControllerSpecs: []*cpb.ControllerSpec{{
				Spec: &cpb.ControllerSpec_Ixiatg{
					&cpb.IxiaTGSpec{
						ManifestDir: "/foo",
					},
				},
			}},
		},
		wantErr: `failed to validate path "/foo"`,
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
			defaultIxiaTGManifestDir = "/"
			if tt.defaultIxiaTGManifestDirDNE {
				defaultIxiaTGManifestDir = "/this/path/dne"
			}
			got, err := newDeployment(tt.req)
			if s := errdiff.Substring(err, tt.wantErr); s != "" {
				t.Fatalf("newDeployment() unexpected error: %s", s)
			}
			ignore := cmpopts.IgnoreUnexported(
				deploy.KindSpec{},
				deploy.MeshnetSpec{},
				deploy.MetalLBSpec{},
				deploy.IxiaTGSpec{},
			)
			if s := cmp.Diff(tt.want, got, ignore); s != "" {
				t.Errorf("newDeployment() unexpected diff (-want +got):\n%s", s)
			}
		})
	}
}
