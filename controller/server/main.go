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
	"path/filepath"

	log "github.com/sirupsen/logrus"
	"k8s.io/client-go/util/homedir"

	"github.com/google/kne/deploy"
	cpb "github.com/google/kne/proto/controller"

	"google.golang.org/grpc/credentials/alts"

	"google.golang.org/grpc"
)

const (
	defaultMetallbManifestDir = "../../manifests/metallb"
	defaultMeshnetManifestDir = "../../manifests/meshnet/base"
)

var (
	port           = flag.Int("port", 50051, "Controller server port")
	defaultKubeCfg = ""
)

type server struct {
	cpb.UnimplementedTopologyManagerServer
}

func getDeployment(req *cpb.CreateClusterRequest) (*deploy.Deployment, error) {
	d := &deploy.Deployment{}
	switch kind := req.ClusterSpec.(type) {
	case *cpb.CreateClusterRequest_Kind:
		k := &deploy.KindSpec{}
		k.Name = req.GetKind().Name
		k.Recycle = req.GetKind().Recycle
		k.Version = req.GetKind().Version
		k.Image = req.GetKind().Image
		d.Cluster = k
	default:
		return nil, fmt.Errorf("cluster type not supported: %T", kind)
	}
	switch metallb := req.IngressSpec.(type) {
	case *cpb.CreateClusterRequest_Metallb:
		l := &deploy.MetalLBSpec{}
		if req.GetMetallb().ManifestDir == "" {
			l.ManifestDir = defaultMetallbManifestDir
		} else {
			l.ManifestDir = req.GetMetallb().ManifestDir
		}
		l.IPCount = int(req.GetMetallb().IpCount)
		d.Ingress = l
	default:
		return nil, fmt.Errorf("ingress spec not supported: %T", metallb)
	}
	switch meshnet := req.CniSpec.(type) {
	case *cpb.CreateClusterRequest_Meshnet:
		m := &deploy.MeshnetSpec{}
		if req.GetMeshnet().ManifestDir == "" {
			m.ManifestDir = defaultMeshnetManifestDir
		} else {
			m.ManifestDir = req.GetMeshnet().ManifestDir
		}
		d.CNI = m
	default:
		return nil, fmt.Errorf("CNI type not supported: %T", meshnet)
	}
	return d, nil

}

func (s *server) CreateCluster(ctx context.Context, req *cpb.CreateClusterRequest) (*cpb.CreateClusterResponse, error) {
	log.Infof("Creating new cluster")
	d, err := getDeployment(req)
	if err != nil {
		return nil, err
	}
	if home := homedir.HomeDir(); home != "" {
		defaultKubeCfg = filepath.Join(home, ".kube", "config")
	}
	if err := d.Deploy(ctx, defaultKubeCfg); err != nil {
		return &cpb.CreateClusterResponse{
			Name:  req.GetKind().Name,
			State: cpb.ClusterState_CLUSTER_STATE_ERROR,
		}, fmt.Errorf("Failed to deploy cluster: %s", err)
	}
	log.Infof("Cluster deployed and ready for topology.")
	return &cpb.CreateClusterResponse{
		Name:  req.GetKind().Name,
		State: cpb.ClusterState_CLUSTER_STATE_RUNNING,
	}, nil
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
	cpb.RegisterTopologyManagerServer(s, &server{})
	log.Printf("Controller server listening at %v", lis.Addr())
	if err := s.Serve(lis); err != nil {
		log.Fatalf("failed to serve: %v", err)
	}
}
