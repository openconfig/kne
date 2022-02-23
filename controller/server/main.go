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
	"sync"

	log "github.com/golang/glog"
	"github.com/google/kne/deploy"
	cpb "github.com/google/kne/proto/controller"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials/alts"
	"google.golang.org/grpc/status"
	"k8s.io/client-go/util/homedir"
)

var (
	defaultKubeCfg            = ""
	defaultMetallbManifestDir = ""
	defaultMeshnetManifestDir = ""
	// Flags.
	port = flag.Int("port", 50051, "Controller server port")
)

func init() {
	if home := homedir.HomeDir(); home != "" {
		defaultKubeCfg = filepath.Join(home, ".kube", "config")
		defaultMeshnetManifestDir = filepath.Join(home, "kne", "manifests", "meshnet", "base")
		defaultMetallbManifestDir = filepath.Join(home, "kne", "manifests", "metallb")
	}
}

type server struct {
	cpb.UnimplementedTopologyManagerServer

	mu sync.Mutex
	deployments map[string]*deploy.Deployment
}

func newServer() *server {
	return &server{deployments: map[string]*deploy.Deployment{}}
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
			ContainerImages:          req.GetKind().ContainerImages,
		}
	default:
		return nil, fmt.Errorf("cluster type not supported: %T", kind)
	}
	switch metallb := req.IngressSpec.(type) {
	case *cpb.CreateClusterRequest_Metallb:
		l := &deploy.MetalLBSpec{}
		var path string
		path = defaultMetallbManifestDir
		if req.GetMetallb().ManifestDir != "" {
			path = req.GetMetallb().ManifestDir
		}
		p, err := validatePath(path)
		if err != nil {
			return nil, fmt.Errorf("failed to validate path %q", path)
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
		var path string
		path = defaultMeshnetManifestDir
		if req.GetMeshnet().ManifestDir != "" {
			path = req.GetMeshnet().ManifestDir
		}
		p, err := validatePath(path)
		if err != nil {
			return nil, fmt.Errorf("failed to validate path %q", path)
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
	log.Infof("Received CreateCluster request: %+v", req)
	d, err := newDeployment(req)
	if err != nil {
		return nil, status.Errorf(codes.InvalidArgument, "unable to parse request: %v", err)
	}
	log.Infof("Parsed request into deployment: %v", d)
	if err := d.Deploy(ctx, defaultKubeCfg); err != nil {
		resp := &cpb.CreateClusterResponse{
			Name:  d.Cluster.GetName(),
			State: cpb.ClusterState_CLUSTER_STATE_ERROR,
		}
		return resp, status.Errorf(codes.Internal, "failed to deploy cluster: %v", err)
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.deployments[d.Cluster.GetName()] = d
	log.Infof("Cluster %q deployed and ready for topology", d.Cluster.GetName())
	resp := &cpb.CreateClusterResponse{
		Name:  d.Cluster.GetName(),
		State: cpb.ClusterState_CLUSTER_STATE_RUNNING,
	}
	return resp, nil
}

func (s *server) DeleteCluster(ctx context.Context, req *cpb.DeleteClusterRequest) (*cpb.DeleteClusterResponse, error) {
	log.Infof("Received DeleteCluster request: %+v", req)
	s.mu.Lock()
	defer s.mu.Unlock()
	d, ok := s.deployments[req.GetName()]
	if !ok {
		return nil, status.Errorf(codes.NotFound, "cluster %q not found, can only delete clusters created using TopologyManager")
	}
	if err := d.Delete(); err != nil {
		return nil, status.Errorf(codes.Internal, "failed to delete cluster: %v", err)
	}
	delete(s.deployments, req.GetName())
	log.Infof("Deleted cluster %q", d.Cluster.GetName())
	return &cpb.DeleteClusterResponse{}, nil
}

func (s *server) ShowCluster(ctx context.Context, req *cpb.ShowClusterRequest) (*cpb.ShowClusterResponse, error) {
	log.Infof("Received ShowCluster request: %+v", req)
	s.mu.Lock()
	defer s.mu.Unlock()
	d, ok := s.deployments[req.GetName()]
	if !ok {
		return nil, status.Errorf(codes.NotFound, "cluster %q not found, can only show clusters created using TopologyManager")
	}
	if err := d.Healthy(ctx); err != nil {
		return &cpb.ShowClusterResponse{State: cpb.ClusterState_CLUSTER_STATE_ERROR}, nil
	}
	return &cpb.ShowClusterResponse{State: cpb.ClusterState_CLUSTER_STATE_RUNNING}, nil
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

func main() {
	flag.Parse()
	addr := fmt.Sprintf(":%d", *port)
	lis, err := net.Listen("tcp6", addr)
	if err != nil {
		log.Fatalf("failed to listen: %v", err)
	}
	creds := alts.NewServerCreds(alts.DefaultServerOptions())
	s := grpc.NewServer(grpc.Creds(creds))
	cpb.RegisterTopologyManagerServer(s, newServer())
	log.Infof("Controller server listening at %v", lis.Addr())
	if err := s.Serve(lis); err != nil {
		log.Fatalf("failed to serve: %v", err)
	}
}
