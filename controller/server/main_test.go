package main

import (
	"testing"

	cpb "github.com/google/kne/proto/controller"
	"github.com/h-fam/errdiff"
)

func TestGetDeployment(t *testing.T) {

	tests := []struct {
		desc    string
		req     *cpb.CreateClusterRequest
		wantErr string
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
					ManifestDir: "../../foo",
					IpCount:     100,
				},
			},
			CniSpec: &cpb.CreateClusterRequest_Meshnet{
				&cpb.MeshnetSpec{
					ManifestDir: "../../bar",
				},
			},
		},
	}, {
		desc:    "empty kind spec",
		req:     &cpb.CreateClusterRequest{},
		wantErr: "cluster type not supported:",
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
		wantErr: "ingress spec not supported:",
	}, {
		desc: "empty Cni spec",
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
					ManifestDir: "../../foo",
					IpCount:     100,
				},
			},
		},
		wantErr: "CNI type not supported:",
	}}
	for _, tt := range tests {
		t.Run(tt.desc, func(t *testing.T) {
			d, err := getDeployment(tt.req)
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
