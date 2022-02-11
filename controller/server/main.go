// Copyright 2021 Google LLC
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//      http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.
package main

import (
	"context"
	"flag"
	"fmt"
	"net"
	"os"
	"path/filepath"

	"github.com/google/kne/deploy"
	cpb "github.com/google/kne/proto/controller"
	log "github.com/sirupsen/logrus"
	"k8s.io/client-go/util/homedir"

	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/alts"
)

var (
	defaultKubeCfg            = ""
	port                      = flag.Int("port", 50051, "Controller server port")
	defaultMetallbManifestDir = ""
	defaultMeshnetManifestDir = ""
)

type server struct {
	cpb.UnimplementedTopologyManagerServer
}

func newDeployment(req *cpb.CreateClusterRequest) (*deploy.Deployment, error) {
	d := &deploy.Deployment{}
	switch kind := req.ClusterSpec.(type) {
	case *cpb.CreateClusterRequest_Kind:
		d.Cluster = &deploy.KindSpec{
			Name:                     req.GetKind().Name,
			Recycle:                  req.GetKind().Recycle,
			Version:                  req.GetKind().Version,
			Image:                    req.GetKind().Image,
			Retain:                   req.GetKind().Retain,
			GoogleArtifactRegistries: req.GetKind().GoogleArtifactRegistries,
		}
	default:
		return nil, fmt.Errorf("cluster type not supported: %T", kind)
	}
	switch metallb := req.IngressSpec.(type) {
	case *cpb.CreateClusterRequest_Metallb:
		l := &deploy.MetalLBSpec{}
		if req.GetMetallb().ManifestDir != "" {
			defaultMetallbManifestDir = req.GetMetallb().ManifestDir

		}
		p, err := validatePath(defaultMetallbManifestDir)
		if err != nil {
			return nil, fmt.Errorf("failed to validate path %q", defaultMetallbManifestDir)
		}
		l.ManifestDir = p
		l.IPCount = int(req.GetMetallb().IpCount)
		l.Version = req.GetMetallb().Version
		d.Ingress = l
	default:
		return nil, fmt.Errorf("ingress spec not supported: %T", metallb)
	}
	switch meshnet := req.CniSpec.(type) {
	case *cpb.CreateClusterRequest_Meshnet:
		m := &deploy.MeshnetSpec{}
		if req.GetMeshnet().ManifestDir != "" {
			defaultMeshnetManifestDir = req.GetMeshnet().ManifestDir
		}
		p, err := validatePath(defaultMeshnetManifestDir)
		if err != nil {
			return nil, fmt.Errorf("failed to validate path %q", defaultMeshnetManifestDir)
		}
		m.Image = req.GetMeshnet().Image
		m.ManifestDir = p
		d.CNI = m
	default:
		return nil, fmt.Errorf("cni type not supported: %T", meshnet)
	}
	return d, nil

}

func (s *server) CreateCluster(ctx context.Context, req *cpb.CreateClusterRequest) (*cpb.CreateClusterResponse, error) {
	log.Infof("Creating new cluster")
	d, err := newDeployment(req)
	if err != nil {
		return nil, err
	}
	if err := d.Deploy(ctx, defaultKubeCfg); err != nil {
		resp := &cpb.CreateClusterResponse{
			Name:  req.GetKind().Name,
			State: cpb.ClusterState_CLUSTER_STATE_ERROR,
		}
		return resp, fmt.Errorf("failed to deploy cluster: %s", err)
	}
	log.Infof("Cluster: %q deployed and ready for topology.", req.GetKind().Name)
	resp := &cpb.CreateClusterResponse{
		Name:  req.GetKind().Name,
		State: cpb.ClusterState_CLUSTER_STATE_RUNNING,
	}
	return resp, nil
}

func validatePath(path string) (string, error) {
	path, err := filepath.Abs(path)
	if err != nil {
		return "", fmt.Errorf("failed to evaluate absolute path: %q", path)
	}
	if _, err := os.Stat(path); err != nil {
		return "", fmt.Errorf("path %q does not exist", path)
	}
	return path, nil
}

func Init() {
	if home := homedir.HomeDir(); home != "" {
		defaultKubeCfg = filepath.Join(home, ".kube", "config")
		defaultMeshnetManifestDir = filepath.Join(home, "kne/manifests/meshnet/base")
		defaultMetallbManifestDir = filepath.Join(home, "kne/manifests/metallb")
	}

}

func main() {
	Init()
	flag.Parse()
	addr := fmt.Sprintf(":%d", *port)
	lis, err := net.Listen("tcp6", addr)
	if err != nil {
		log.Fatalf("failed to listen: %v", err)
	}
	creds := alts.NewServerCreds(alts.DefaultServerOptions())
	s := grpc.NewServer(grpc.Creds(creds))
	cpb.RegisterTopologyManagerServer(s, &server{})
	log.Printf("Controller server listening at %v", lis.Addr())
	if err := s.Serve(lis); err != nil {
		log.Fatalf("failed to serve: %v", err)
	}
}
