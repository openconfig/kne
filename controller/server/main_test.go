package main

import (
	"os"
	"testing"

	"github.com/google/go-cmp/cmp"
	"github.com/google/go-cmp/cmp/cmpopts"
	"github.com/h-fam/errdiff"
	"github.com/openconfig/kne/deploy"
	cpb "github.com/openconfig/kne/proto/controller"
)

func TestNewDeployment(t *testing.T) {
	testData := []byte("testdata")
	testFile, err := os.CreateTemp("", "testfile")
	if err != nil {
		t.Fatalf("failed to create test file: %v", err)
	}
	defer func() {
		os.RemoveAll(testFile.Name())
	}()
	defTestFile, err := os.CreateTemp("", "deftestfile")
	if err != nil {
		t.Fatalf("failed to create default test file: %v", err)
	}
	defer func() {
		os.RemoveAll(defTestFile.Name())
	}()

	mlb := defaultMetalLBManifest
	defer func() {
		defaultMetalLBManifest = mlb
	}()
	m := defaultMeshnetManifest
	defer func() {
		defaultMeshnetManifest = m
	}()
	itgo := defaultIxiaTGOperator
	defer func() {
		defaultIxiaTGOperator = itgo
	}()
	itgcm := defaultIxiaTGConfigMap
	defer func() {
		defaultIxiaTGOperator = itgcm
	}()
	srl := defaultSRLinuxOperator
	defer func() {
		defaultSRLinuxOperator = srl
	}()
	ceos := defaultCEOSLabOperator
	defer func() {
		defaultCEOSLabOperator = ceos
	}()
	lem := defaultLemmingOperator
	defer func() {
		defaultLemmingOperator = lem
	}()

	tests := []struct {
		desc                      string
		req                       *cpb.CreateClusterRequest
		defaultMeshnetManifestDNE bool
		defaultMetalLBManifestDNE bool
		defaultIxiaTGOperatorDNE  bool
		defaultIxiaTGConfigMapDNE bool
		defaultSRLinuxOperatorDNE bool
		defaultCEOSLabOperatorDNE bool
		defaultLemmingOperatorDNE bool
		want                      *deploy.Deployment
		wantErr                   string
	}{{
		desc: "request spec",
		req: &cpb.CreateClusterRequest{
			ClusterSpec: &cpb.CreateClusterRequest_Kind{
				Kind: &cpb.KindSpec{
					Name:    "kne",
					Recycle: true,
					Version: "0.11.1",
					Image:   "kindest/node:v1.22.1",
				},
			},
			IngressSpec: &cpb.CreateClusterRequest_Metallb{
				Metallb: &cpb.MetallbSpec{
					Manifest: &cpb.Manifest{
						ManifestData: &cpb.Manifest_File{
							testFile.Name(),
						},
					},
					IpCount: 100,
				},
			},
			CniSpec: &cpb.CreateClusterRequest_Meshnet{
				Meshnet: &cpb.MeshnetSpec{
					Manifest: &cpb.Manifest{
						ManifestData: &cpb.Manifest_File{
							testFile.Name(),
						},
					},
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
				Manifest: testFile.Name(),
				IPCount:  100,
			},
			CNI: &deploy.MeshnetSpec{
				Manifest: testFile.Name(),
			},
		},
	}, {
		desc: "request spec - with ixiatg controller",
		req: &cpb.CreateClusterRequest{
			ClusterSpec: &cpb.CreateClusterRequest_Kind{
				Kind: &cpb.KindSpec{
					Name:    "kne",
					Recycle: true,
					Version: "0.11.1",
					Image:   "kindest/node:v1.22.1",
				},
			},
			IngressSpec: &cpb.CreateClusterRequest_Metallb{
				Metallb: &cpb.MetallbSpec{
					Manifest: &cpb.Manifest{
						ManifestData: &cpb.Manifest_File{
							testFile.Name(),
						},
					},
					IpCount: 100,
				},
			},
			CniSpec: &cpb.CreateClusterRequest_Meshnet{
				Meshnet: &cpb.MeshnetSpec{
					Manifest: &cpb.Manifest{
						ManifestData: &cpb.Manifest_File{
							testFile.Name(),
						},
					},
				},
			},
			ControllerSpecs: []*cpb.ControllerSpec{{
				Spec: &cpb.ControllerSpec_Ixiatg{
					Ixiatg: &cpb.IxiaTGSpec{
						Operator: &cpb.Manifest{
							ManifestData: &cpb.Manifest_File{
								testFile.Name(),
							},
						},
						ConfigMap: &cpb.Manifest{
							ManifestData: &cpb.Manifest_File{
								testFile.Name(),
							},
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
				Manifest: testFile.Name(),
				IPCount:  100,
			},
			CNI: &deploy.MeshnetSpec{
				Manifest: testFile.Name(),
			},
			Controllers: []deploy.Controller{
				&deploy.IxiaTGSpec{
					Operator:  testFile.Name(),
					ConfigMap: testFile.Name(),
				},
			},
		},
	}, {
		desc: "request spec - with srlinux controller",
		req: &cpb.CreateClusterRequest{
			ClusterSpec: &cpb.CreateClusterRequest_Kind{
				Kind: &cpb.KindSpec{
					Name:    "kne",
					Recycle: true,
					Version: "0.11.1",
					Image:   "kindest/node:v1.22.1",
				},
			},
			IngressSpec: &cpb.CreateClusterRequest_Metallb{
				Metallb: &cpb.MetallbSpec{
					Manifest: &cpb.Manifest{
						ManifestData: &cpb.Manifest_File{
							testFile.Name(),
						},
					},
					IpCount: 100,
				},
			},
			CniSpec: &cpb.CreateClusterRequest_Meshnet{
				Meshnet: &cpb.MeshnetSpec{
					Manifest: &cpb.Manifest{
						ManifestData: &cpb.Manifest_File{
							testFile.Name(),
						},
					},
				},
			},
			ControllerSpecs: []*cpb.ControllerSpec{{
				Spec: &cpb.ControllerSpec_Srlinux{
					Srlinux: &cpb.SRLinuxSpec{
						Operator: &cpb.Manifest{
							ManifestData: &cpb.Manifest_File{
								testFile.Name(),
							},
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
				Manifest: testFile.Name(),
				IPCount:  100,
			},
			CNI: &deploy.MeshnetSpec{
				Manifest: testFile.Name(),
			},
			Controllers: []deploy.Controller{
				&deploy.SRLinuxSpec{
					Operator: testFile.Name(),
				},
			},
		},
	}, {
		desc: "request spec - with ceoslab controller",
		req: &cpb.CreateClusterRequest{
			ClusterSpec: &cpb.CreateClusterRequest_Kind{
				Kind: &cpb.KindSpec{
					Name:    "kne",
					Recycle: true,
					Version: "0.11.1",
					Image:   "kindest/node:v1.22.1",
				},
			},
			IngressSpec: &cpb.CreateClusterRequest_Metallb{
				Metallb: &cpb.MetallbSpec{
					Manifest: &cpb.Manifest{
						ManifestData: &cpb.Manifest_File{
							testFile.Name(),
						},
					},
					IpCount: 100,
				},
			},
			CniSpec: &cpb.CreateClusterRequest_Meshnet{
				Meshnet: &cpb.MeshnetSpec{
					Manifest: &cpb.Manifest{
						ManifestData: &cpb.Manifest_File{
							testFile.Name(),
						},
					},
				},
			},
			ControllerSpecs: []*cpb.ControllerSpec{{
				Spec: &cpb.ControllerSpec_Ceoslab{
					Ceoslab: &cpb.CEOSLabSpec{
						Operator: &cpb.Manifest{
							ManifestData: &cpb.Manifest_File{
								testFile.Name(),
							},
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
				Manifest: testFile.Name(),
				IPCount:  100,
			},
			CNI: &deploy.MeshnetSpec{
				Manifest: testFile.Name(),
			},
			Controllers: []deploy.Controller{
				&deploy.CEOSLabSpec{
					Operator: testFile.Name(),
				},
			},
		},
	}, {
		desc: "request spec - with lemming controller",
		req: &cpb.CreateClusterRequest{
			ClusterSpec: &cpb.CreateClusterRequest_Kind{
				Kind: &cpb.KindSpec{
					Name:    "kne",
					Recycle: true,
					Version: "0.11.1",
					Image:   "kindest/node:v1.22.1",
				},
			},
			IngressSpec: &cpb.CreateClusterRequest_Metallb{
				Metallb: &cpb.MetallbSpec{
					Manifest: &cpb.Manifest{
						ManifestData: &cpb.Manifest_File{
							testFile.Name(),
						},
					},
					IpCount: 100,
				},
			},
			CniSpec: &cpb.CreateClusterRequest_Meshnet{
				Meshnet: &cpb.MeshnetSpec{
					Manifest: &cpb.Manifest{
						ManifestData: &cpb.Manifest_File{
							testFile.Name(),
						},
					},
				},
			},
			ControllerSpecs: []*cpb.ControllerSpec{{
				Spec: &cpb.ControllerSpec_Lemming{
					Lemming: &cpb.LemmingSpec{
						Operator: &cpb.Manifest{
							ManifestData: &cpb.Manifest_File{
								testFile.Name(),
							},
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
				Manifest: testFile.Name(),
				IPCount:  100,
			},
			CNI: &deploy.MeshnetSpec{
				Manifest: testFile.Name(),
			},
			Controllers: []deploy.Controller{
				&deploy.LemmingSpec{
					Operator: testFile.Name(),
				},
			},
		},
	}, {
		desc: "request spec - with multiple controllers empty filepath",
		req: &cpb.CreateClusterRequest{
			ClusterSpec: &cpb.CreateClusterRequest_Kind{
				Kind: &cpb.KindSpec{
					Name:    "kne",
					Recycle: true,
					Version: "0.11.1",
					Image:   "kindest/node:v1.22.1",
				},
			},
			IngressSpec: &cpb.CreateClusterRequest_Metallb{
				Metallb: &cpb.MetallbSpec{
					Manifest: &cpb.Manifest{
						ManifestData: &cpb.Manifest_File{},
					},
					IpCount: 100,
				},
			},
			CniSpec: &cpb.CreateClusterRequest_Meshnet{
				Meshnet: &cpb.MeshnetSpec{
					Manifest: &cpb.Manifest{
						ManifestData: &cpb.Manifest_File{},
					},
				},
			},
			ControllerSpecs: []*cpb.ControllerSpec{
				{
					Spec: &cpb.ControllerSpec_Ixiatg{
						Ixiatg: &cpb.IxiaTGSpec{
							Operator: &cpb.Manifest{
								ManifestData: &cpb.Manifest_File{},
							},
							ConfigMap: &cpb.Manifest{
								ManifestData: &cpb.Manifest_File{},
							},
						},
					},
				},
				{
					Spec: &cpb.ControllerSpec_Srlinux{
						Srlinux: &cpb.SRLinuxSpec{
							Operator: &cpb.Manifest{
								ManifestData: &cpb.Manifest_File{},
							},
						},
					},
				},
				{
					Spec: &cpb.ControllerSpec_Ceoslab{
						Ceoslab: &cpb.CEOSLabSpec{
							Operator: &cpb.Manifest{
								ManifestData: &cpb.Manifest_File{},
							},
						},
					},
				},
				{
					Spec: &cpb.ControllerSpec_Lemming{
						Lemming: &cpb.LemmingSpec{
							Operator: &cpb.Manifest{
								ManifestData: &cpb.Manifest_File{},
							},
						},
					},
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
				Manifest: defTestFile.Name(),
				IPCount:  100,
			},
			CNI: &deploy.MeshnetSpec{
				Manifest: defTestFile.Name(),
			},
			Controllers: []deploy.Controller{
				&deploy.IxiaTGSpec{
					Operator:  defTestFile.Name(),
					ConfigMap: defTestFile.Name(),
				},
				&deploy.SRLinuxSpec{
					Operator: defTestFile.Name(),
				},
				&deploy.CEOSLabSpec{
					Operator: defTestFile.Name(),
				},
				&deploy.LemmingSpec{
					Operator: defTestFile.Name(),
				},
			},
		},
	}, {
		desc: "request spec - with multiple controllers",
		req: &cpb.CreateClusterRequest{
			ClusterSpec: &cpb.CreateClusterRequest_Kind{
				Kind: &cpb.KindSpec{
					Name:    "kne",
					Recycle: true,
					Version: "0.11.1",
					Image:   "kindest/node:v1.22.1",
				},
			},
			IngressSpec: &cpb.CreateClusterRequest_Metallb{
				Metallb: &cpb.MetallbSpec{
					Manifest: &cpb.Manifest{
						ManifestData: &cpb.Manifest_File{
							testFile.Name(),
						},
					},
					IpCount: 100,
				},
			},
			CniSpec: &cpb.CreateClusterRequest_Meshnet{
				Meshnet: &cpb.MeshnetSpec{
					Manifest: &cpb.Manifest{
						ManifestData: &cpb.Manifest_File{
							testFile.Name(),
						},
					},
				},
			},
			ControllerSpecs: []*cpb.ControllerSpec{
				{
					Spec: &cpb.ControllerSpec_Ixiatg{
						Ixiatg: &cpb.IxiaTGSpec{
							Operator: &cpb.Manifest{
								ManifestData: &cpb.Manifest_File{
									testFile.Name(),
								},
							},
							ConfigMap: &cpb.Manifest{
								ManifestData: &cpb.Manifest_File{
									testFile.Name(),
								},
							},
						},
					},
				},
				{
					Spec: &cpb.ControllerSpec_Srlinux{
						Srlinux: &cpb.SRLinuxSpec{
							Operator: &cpb.Manifest{
								ManifestData: &cpb.Manifest_File{
									testFile.Name(),
								},
							},
						},
					},
				},
				{
					Spec: &cpb.ControllerSpec_Ceoslab{
						Ceoslab: &cpb.CEOSLabSpec{
							Operator: &cpb.Manifest{
								ManifestData: &cpb.Manifest_File{
									testFile.Name(),
								},
							},
						},
					},
				},
				{
					Spec: &cpb.ControllerSpec_Lemming{
						Lemming: &cpb.LemmingSpec{
							Operator: &cpb.Manifest{
								ManifestData: &cpb.Manifest_File{
									testFile.Name(),
								},
							},
						},
					},
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
				Manifest: testFile.Name(),
				IPCount:  100,
			},
			CNI: &deploy.MeshnetSpec{
				Manifest: testFile.Name(),
			},
			Controllers: []deploy.Controller{
				&deploy.IxiaTGSpec{
					Operator:  testFile.Name(),
					ConfigMap: testFile.Name(),
				},
				&deploy.SRLinuxSpec{
					Operator: testFile.Name(),
				},
				&deploy.CEOSLabSpec{
					Operator: testFile.Name(),
				},
				&deploy.LemmingSpec{
					Operator: testFile.Name(),
				},
			},
		},
	}, {
		desc: "request spec - with multiple controllers data",
		req: &cpb.CreateClusterRequest{
			ClusterSpec: &cpb.CreateClusterRequest_Kind{
				Kind: &cpb.KindSpec{
					Name:    "kne",
					Recycle: true,
					Version: "0.11.1",
					Image:   "kindest/node:v1.22.1",
				},
			},
			IngressSpec: &cpb.CreateClusterRequest_Metallb{
				Metallb: &cpb.MetallbSpec{
					Manifest: &cpb.Manifest{
						ManifestData: &cpb.Manifest_Data{
							testData,
						},
					},
					IpCount: 100,
				},
			},
			CniSpec: &cpb.CreateClusterRequest_Meshnet{
				Meshnet: &cpb.MeshnetSpec{
					Manifest: &cpb.Manifest{
						ManifestData: &cpb.Manifest_Data{
							testData,
						},
					},
				},
			},
			ControllerSpecs: []*cpb.ControllerSpec{
				{
					Spec: &cpb.ControllerSpec_Ixiatg{
						Ixiatg: &cpb.IxiaTGSpec{
							Operator: &cpb.Manifest{
								ManifestData: &cpb.Manifest_Data{
									testData,
								},
							},
							ConfigMap: &cpb.Manifest{
								ManifestData: &cpb.Manifest_Data{
									testData,
								},
							},
						},
					},
				},
				{
					Spec: &cpb.ControllerSpec_Srlinux{
						Srlinux: &cpb.SRLinuxSpec{
							Operator: &cpb.Manifest{
								ManifestData: &cpb.Manifest_Data{
									testData,
								},
							},
						},
					},
				},
				{
					Spec: &cpb.ControllerSpec_Ceoslab{
						Ceoslab: &cpb.CEOSLabSpec{
							Operator: &cpb.Manifest{
								ManifestData: &cpb.Manifest_Data{
									testData,
								},
							},
						},
					},
				},
				{
					Spec: &cpb.ControllerSpec_Lemming{
						Lemming: &cpb.LemmingSpec{
							Operator: &cpb.Manifest{
								ManifestData: &cpb.Manifest_Data{
									testData,
								},
							},
						},
					},
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
				ManifestData: testData,
				IPCount:      100,
			},
			CNI: &deploy.MeshnetSpec{
				ManifestData: testData,
			},
			Controllers: []deploy.Controller{
				&deploy.IxiaTGSpec{
					OperatorData:  testData,
					ConfigMapData: testData,
				},
				&deploy.SRLinuxSpec{
					OperatorData: testData,
				},
				&deploy.CEOSLabSpec{
					OperatorData: testData,
				},
				&deploy.LemmingSpec{
					OperatorData: testData,
				},
			},
		},
	}, {
		desc: "request spec - without ixiatg config map",
		req: &cpb.CreateClusterRequest{
			ClusterSpec: &cpb.CreateClusterRequest_Kind{
				Kind: &cpb.KindSpec{
					Name:    "kne",
					Recycle: true,
					Version: "0.11.1",
					Image:   "kindest/node:v1.22.1",
				},
			},
			IngressSpec: &cpb.CreateClusterRequest_Metallb{
				Metallb: &cpb.MetallbSpec{
					Manifest: &cpb.Manifest{
						ManifestData: &cpb.Manifest_File{
							testFile.Name(),
						},
					},
					IpCount: 100,
				},
			},
			CniSpec: &cpb.CreateClusterRequest_Meshnet{
				Meshnet: &cpb.MeshnetSpec{
					Manifest: &cpb.Manifest{
						ManifestData: &cpb.Manifest_File{
							testFile.Name(),
						},
					},
				},
			},
			ControllerSpecs: []*cpb.ControllerSpec{{
				Spec: &cpb.ControllerSpec_Ixiatg{
					Ixiatg: &cpb.IxiaTGSpec{
						Operator: &cpb.Manifest{
							ManifestData: &cpb.Manifest_File{
								testFile.Name(),
							},
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
				Manifest: testFile.Name(),
				IPCount:  100,
			},
			CNI: &deploy.MeshnetSpec{
				Manifest: testFile.Name(),
			},
			Controllers: []deploy.Controller{
				&deploy.IxiaTGSpec{
					Operator:  testFile.Name(),
					ConfigMap: defTestFile.Name(),
				},
			},
		},
	}, {
		desc: "request spec - default ixiatg config map dne",
		req: &cpb.CreateClusterRequest{
			ClusterSpec: &cpb.CreateClusterRequest_Kind{
				Kind: &cpb.KindSpec{
					Name:    "kne",
					Recycle: true,
					Version: "0.11.1",
					Image:   "kindest/node:v1.22.1",
				},
			},
			IngressSpec: &cpb.CreateClusterRequest_Metallb{
				Metallb: &cpb.MetallbSpec{
					Manifest: &cpb.Manifest{
						ManifestData: &cpb.Manifest_File{
							testFile.Name(),
						},
					},
					IpCount: 100,
				},
			},
			CniSpec: &cpb.CreateClusterRequest_Meshnet{
				Meshnet: &cpb.MeshnetSpec{
					Manifest: &cpb.Manifest{
						ManifestData: &cpb.Manifest_File{
							testFile.Name(),
						},
					},
				},
			},
			ControllerSpecs: []*cpb.ControllerSpec{{
				Spec: &cpb.ControllerSpec_Ixiatg{
					Ixiatg: &cpb.IxiaTGSpec{
						Operator: &cpb.Manifest{
							ManifestData: &cpb.Manifest_File{
								testFile.Name(),
							},
						},
					},
				},
			}},
		},
		defaultIxiaTGConfigMapDNE: true,
		wantErr:                   "failed to validate path",
	}, {
		desc: "request spec - default manifest paths",
		req: &cpb.CreateClusterRequest{
			ClusterSpec: &cpb.CreateClusterRequest_Kind{
				Kind: &cpb.KindSpec{
					Name:    "kne",
					Recycle: true,
					Version: "0.11.1",
					Image:   "kindest/node:v1.22.1",
				},
			},
			IngressSpec: &cpb.CreateClusterRequest_Metallb{
				Metallb: &cpb.MetallbSpec{
					IpCount: 100,
				},
			},
			CniSpec: &cpb.CreateClusterRequest_Meshnet{
				Meshnet: &cpb.MeshnetSpec{},
			},
			ControllerSpecs: []*cpb.ControllerSpec{{
				Spec: &cpb.ControllerSpec_Ceoslab{
					Ceoslab: &cpb.CEOSLabSpec{},
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
				Manifest: defTestFile.Name(),
				IPCount:  100,
			},
			CNI: &deploy.MeshnetSpec{
				Manifest: defTestFile.Name(),
			},
			Controllers: []deploy.Controller{
				&deploy.CEOSLabSpec{
					Operator: defTestFile.Name(),
				},
			},
		},
	}, {
		desc: "request spec - default manifest paths dne",
		req: &cpb.CreateClusterRequest{
			ClusterSpec: &cpb.CreateClusterRequest_Kind{
				Kind: &cpb.KindSpec{
					Name:    "kne",
					Recycle: true,
					Version: "0.11.1",
					Image:   "kindest/node:v1.22.1",
				},
			},
			IngressSpec: &cpb.CreateClusterRequest_Metallb{
				Metallb: &cpb.MetallbSpec{
					IpCount: 100,
				},
			},
			CniSpec: &cpb.CreateClusterRequest_Meshnet{
				Meshnet: &cpb.MeshnetSpec{},
			},
			ControllerSpecs: []*cpb.ControllerSpec{{
				Spec: &cpb.ControllerSpec_Ceoslab{
					Ceoslab: &cpb.CEOSLabSpec{},
				},
			}},
		},
		defaultMeshnetManifestDNE: true,
		defaultMetalLBManifestDNE: true,
		defaultCEOSLabOperatorDNE: true,
		wantErr:                   "failed to validate path",
	}, {
		desc:    "empty kind spec",
		req:     &cpb.CreateClusterRequest{},
		wantErr: "cluster type not supported",
	}, {
		desc: "empty ingress spec",
		req: &cpb.CreateClusterRequest{
			ClusterSpec: &cpb.CreateClusterRequest_Kind{
				Kind: &cpb.KindSpec{
					Name:    "kne",
					Recycle: true,
					Version: "0.11.1",
					Image:   "kindest/node:v1.22.1",
				},
			},
			CniSpec: &cpb.CreateClusterRequest_Meshnet{
				Meshnet: &cpb.MeshnetSpec{
					Manifest: &cpb.Manifest{
						ManifestData: &cpb.Manifest_File{
							testFile.Name(),
						},
					},
				},
			},
		},
		wantErr: "ingress spec not supported",
	}, {
		desc: "empty cni spec",
		req: &cpb.CreateClusterRequest{
			ClusterSpec: &cpb.CreateClusterRequest_Kind{
				Kind: &cpb.KindSpec{
					Name:    "kne",
					Recycle: true,
					Version: "0.11.1",
					Image:   "kindest/node:v1.22.1",
				},
			},
			IngressSpec: &cpb.CreateClusterRequest_Metallb{
				Metallb: &cpb.MetallbSpec{
					Manifest: &cpb.Manifest{
						ManifestData: &cpb.Manifest_File{
							testFile.Name(),
						},
					},
					IpCount: 100,
				},
			},
		},
		wantErr: "cni type not supported",
	}, {
		desc: "bad meshnet manifest dir",
		req: &cpb.CreateClusterRequest{
			ClusterSpec: &cpb.CreateClusterRequest_Kind{
				Kind: &cpb.KindSpec{
					Name:    "kne",
					Recycle: true,
					Version: "0.11.1",
					Image:   "kindest/node:v1.22.1",
				},
			},
			IngressSpec: &cpb.CreateClusterRequest_Metallb{
				Metallb: &cpb.MetallbSpec{
					IpCount: 100,
				},
			},
			CniSpec: &cpb.CreateClusterRequest_Meshnet{
				Meshnet: &cpb.MeshnetSpec{
					Manifest: &cpb.Manifest{
						ManifestData: &cpb.Manifest_File{
							"/foo.yaml",
						},
					},
				},
			},
		},
		wantErr: `failed to validate path "/foo.yaml"`,
	}, {
		desc: "bad metallb manifest dir",
		req: &cpb.CreateClusterRequest{
			ClusterSpec: &cpb.CreateClusterRequest_Kind{
				Kind: &cpb.KindSpec{
					Name:    "kne",
					Recycle: true,
					Version: "0.11.1",
					Image:   "kindest/node:v1.22.1",
				},
			},
			IngressSpec: &cpb.CreateClusterRequest_Metallb{
				Metallb: &cpb.MetallbSpec{
					Manifest: &cpb.Manifest{
						ManifestData: &cpb.Manifest_File{
							"/foo.yaml",
						},
					},
					IpCount: 100,
				},
			},
			CniSpec: &cpb.CreateClusterRequest_Meshnet{
				Meshnet: &cpb.MeshnetSpec{},
			},
		},
		wantErr: `failed to validate path "/foo.yaml"`,
	}, {
		desc: "bad ixiatg operator path",
		req: &cpb.CreateClusterRequest{
			ClusterSpec: &cpb.CreateClusterRequest_Kind{
				Kind: &cpb.KindSpec{
					Name:    "kne",
					Recycle: true,
					Version: "0.11.1",
					Image:   "kindest/node:v1.22.1",
				},
			},
			IngressSpec: &cpb.CreateClusterRequest_Metallb{
				Metallb: &cpb.MetallbSpec{
					IpCount: 100,
				},
			},
			CniSpec: &cpb.CreateClusterRequest_Meshnet{
				Meshnet: &cpb.MeshnetSpec{},
			},
			ControllerSpecs: []*cpb.ControllerSpec{{
				Spec: &cpb.ControllerSpec_Ixiatg{
					Ixiatg: &cpb.IxiaTGSpec{
						Operator: &cpb.Manifest{
							ManifestData: &cpb.Manifest_File{
								"/foo.yaml",
							},
						},
					},
				},
			}},
		},
		wantErr: `failed to validate path "/foo.yaml"`,
	}, {
		desc: "bad srlinux operator path",
		req: &cpb.CreateClusterRequest{
			ClusterSpec: &cpb.CreateClusterRequest_Kind{
				Kind: &cpb.KindSpec{
					Name:    "kne",
					Recycle: true,
					Version: "0.11.1",
					Image:   "kindest/node:v1.22.1",
				},
			},
			IngressSpec: &cpb.CreateClusterRequest_Metallb{
				Metallb: &cpb.MetallbSpec{
					IpCount: 100,
				},
			},
			CniSpec: &cpb.CreateClusterRequest_Meshnet{
				Meshnet: &cpb.MeshnetSpec{},
			},
			ControllerSpecs: []*cpb.ControllerSpec{{
				Spec: &cpb.ControllerSpec_Srlinux{
					Srlinux: &cpb.SRLinuxSpec{
						Operator: &cpb.Manifest{
							ManifestData: &cpb.Manifest_File{
								"/foo.yaml",
							},
						},
					},
				},
			}},
		},
		wantErr: `failed to validate path "/foo.yaml"`,
	}, {
		desc: "bad ceoslab manifest dir",
		req: &cpb.CreateClusterRequest{
			ClusterSpec: &cpb.CreateClusterRequest_Kind{
				Kind: &cpb.KindSpec{
					Name:    "kne",
					Recycle: true,
					Version: "0.11.1",
					Image:   "kindest/node:v1.22.1",
				},
			},
			IngressSpec: &cpb.CreateClusterRequest_Metallb{
				Metallb: &cpb.MetallbSpec{
					IpCount: 100,
				},
			},
			CniSpec: &cpb.CreateClusterRequest_Meshnet{
				Meshnet: &cpb.MeshnetSpec{},
			},
			ControllerSpecs: []*cpb.ControllerSpec{{
				Spec: &cpb.ControllerSpec_Ceoslab{
					Ceoslab: &cpb.CEOSLabSpec{
						Operator: &cpb.Manifest{
							ManifestData: &cpb.Manifest_File{
								"/foo.yaml",
							},
						},
					},
				},
			}},
		},
		wantErr: `failed to validate path "/foo.yaml"`,
	}, {
		desc: "bad lemming manifest dir",
		req: &cpb.CreateClusterRequest{
			ClusterSpec: &cpb.CreateClusterRequest_Kind{
				Kind: &cpb.KindSpec{
					Name:    "kne",
					Recycle: true,
					Version: "0.11.1",
					Image:   "kindest/node:v1.22.1",
				},
			},
			IngressSpec: &cpb.CreateClusterRequest_Metallb{
				Metallb: &cpb.MetallbSpec{
					IpCount: 100,
				},
			},
			CniSpec: &cpb.CreateClusterRequest_Meshnet{
				Meshnet: &cpb.MeshnetSpec{},
			},
			ControllerSpecs: []*cpb.ControllerSpec{{
				Spec: &cpb.ControllerSpec_Lemming{
					Lemming: &cpb.LemmingSpec{
						Operator: &cpb.Manifest{
							ManifestData: &cpb.Manifest_File{
								"/foo.yaml",
							},
						},
					},
				},
			}},
		},
		wantErr: `failed to validate path "/foo.yaml"`,
	}}
	for _, tt := range tests {
		t.Run(tt.desc, func(t *testing.T) {
			defaultMetalLBManifest = defTestFile.Name()
			if tt.defaultMetalLBManifestDNE {
				defaultMetalLBManifest = "/this/path/dne.yaml"
			}
			defaultMeshnetManifest = defTestFile.Name()
			if tt.defaultMeshnetManifestDNE {
				defaultMeshnetManifest = "/this/path/dne.yaml"
			}
			defaultIxiaTGOperator = defTestFile.Name()
			if tt.defaultIxiaTGOperatorDNE {
				defaultIxiaTGOperator = "/this/path/dne.yaml"
			}
			defaultIxiaTGConfigMap = defTestFile.Name()
			if tt.defaultIxiaTGConfigMapDNE {
				defaultIxiaTGConfigMap = "/this/path/dne.yaml"
			}
			defaultSRLinuxOperator = defTestFile.Name()
			if tt.defaultSRLinuxOperatorDNE {
				defaultSRLinuxOperator = "/this/path/dne.yaml"
			}
			defaultCEOSLabOperator = defTestFile.Name()
			if tt.defaultCEOSLabOperatorDNE {
				defaultCEOSLabOperator = "/this/path/dne.yaml"
			}
			defaultLemmingOperator = defTestFile.Name()
			if tt.defaultLemmingOperatorDNE {
				defaultLemmingOperator = "/this/path/dne.yaml"
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
				deploy.SRLinuxSpec{},
				deploy.CEOSLabSpec{},
				deploy.LemmingSpec{},
			)
			if s := cmp.Diff(tt.want, got, ignore); s != "" {
				t.Errorf("newDeployment() unexpected diff (-want +got):\n%s", s)
			}
		})
	}
}
