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
package main

import (
	"bytes"
	"context"
	"flag"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"sync"
	"time"

	log "github.com/golang/glog"
	"github.com/openconfig/kne/deploy"
	cpb "github.com/openconfig/kne/proto/controller"
	tpb "github.com/openconfig/kne/proto/topo"
	"github.com/openconfig/kne/topo"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials/alts"
	"google.golang.org/grpc/keepalive"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/encoding/prototext"
	"k8s.io/client-go/util/homedir"
)

var (
	defaultKubeCfg            = ""
	defaultTopoBasePath       = ""
	defaultMetallbManifestDir = ""
	defaultMeshnetManifestDir = ""
	defaultIxiaTGManifestDir  = ""
	defaultSRLinuxManifestDir = ""
	defaultCEOSLabManifestDir = ""
	// Flags.
	port = flag.Int("port", 50051, "Controller server port")
)

func init() {
	if home := homedir.HomeDir(); home != "" {
		defaultKubeCfg = filepath.Join(home, ".kube", "config")
		defaultTopoBasePath = filepath.Join(home, "kne", "examples")
		defaultMeshnetManifestDir = filepath.Join(home, "kne", "manifests", "meshnet", "base")
		defaultMetallbManifestDir = filepath.Join(home, "kne", "manifests", "metallb")
		defaultIxiaTGManifestDir = filepath.Join(home, "keysight", "athena", "operator")
		defaultSRLinuxManifestDir = filepath.Join(home, "srl-controller", "config", "default")
		defaultCEOSLabManifestDir = filepath.Join(home, "arista-ceos-lab", "config", "kustomized")
	}
}

type server struct {
	cpb.UnimplementedTopologyManagerServer

	muDeploy    sync.Mutex // guards deployements map
	deployments map[string]*deploy.Deployment
	muTopo      sync.Mutex        // guards topos map
	topos       map[string][]byte // stores the topology protobuf from the initial topology creation request
}

func newServer() *server {
	return &server{
		deployments: map[string]*deploy.Deployment{},
		topos:       map[string][]byte{},
	}
}

func newDeployment(req *cpb.CreateClusterRequest) (*deploy.Deployment, error) {
	d := &deploy.Deployment{}
	switch t := req.ClusterSpec.(type) {
	case *cpb.CreateClusterRequest_Kind:
		k := &deploy.KindSpec{
			Name:                     req.GetKind().Name,
			Recycle:                  req.GetKind().Recycle,
			Version:                  req.GetKind().Version,
			Image:                    req.GetKind().Image,
			Retain:                   req.GetKind().Retain,
			GoogleArtifactRegistries: req.GetKind().GoogleArtifactRegistries,
			ContainerImages:          req.GetKind().ContainerImages,
			KindConfigFile:           req.GetKind().Config,
			AdditionalManifests:      req.GetKind().AdditionalManifests,
		}
		if k.KindConfigFile != "" {
			p, err := validatePath(k.KindConfigFile)
			if err != nil {
				return nil, fmt.Errorf("failed to validate path %q", p)
			}
			k.KindConfigFile = p
		}
		for i, path := range k.AdditionalManifests {
			p, err := validatePath(path)
			if err != nil {
				return nil, fmt.Errorf("failed to validate path %q", path)
			}
			k.AdditionalManifests[i] = p
		}
		d.Cluster = k
	default:
		return nil, fmt.Errorf("cluster type not supported: %T", t)
	}
	switch t := req.IngressSpec.(type) {
	case *cpb.CreateClusterRequest_Metallb:
		m := &deploy.MetalLBSpec{}
		path := defaultMetallbManifestDir
		if req.GetMetallb().ManifestDir != "" {
			path = req.GetMetallb().ManifestDir
		}
		p, err := validatePath(path)
		if err != nil {
			return nil, fmt.Errorf("failed to validate path %q", path)
		}
		m.ManifestDir = p
		m.IPCount = int(req.GetMetallb().IpCount)
		m.Version = req.GetMetallb().Version
		d.Ingress = m
	default:
		return nil, fmt.Errorf("ingress spec not supported: %T", t)
	}
	switch t := req.CniSpec.(type) {
	case *cpb.CreateClusterRequest_Meshnet:
		m := &deploy.MeshnetSpec{}
		path := defaultMeshnetManifestDir
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
		return nil, fmt.Errorf("cni type not supported: %T", t)
	}
	if len(req.ControllerSpecs) != 0 {
		d.Controllers = []deploy.Controller{}
	}
	for _, cs := range req.ControllerSpecs {
		switch t := cs.Spec.(type) {
		case *cpb.ControllerSpec_Ceoslab:
			c := &deploy.CEOSLabSpec{}
			path := defaultCEOSLabManifestDir
			if cs.GetCeoslab().ManifestDir != "" {
				path = cs.GetCeoslab().ManifestDir
			}
			p, err := validatePath(path)
			if err != nil {
				return nil, fmt.Errorf("failed to validate path %q", path)
			}
			c.ManifestDir = p
			d.Controllers = append(d.Controllers, c)
		case *cpb.ControllerSpec_Srlinux:
			s := &deploy.SRLinuxSpec{}
			path := defaultSRLinuxManifestDir
			if cs.GetSrlinux().ManifestDir != "" {
				path = cs.GetSrlinux().ManifestDir
			}
			p, err := validatePath(path)
			if err != nil {
				return nil, fmt.Errorf("failed to validate path %q", path)
			}
			s.ManifestDir = p
			d.Controllers = append(d.Controllers, s)
		case *cpb.ControllerSpec_Ixiatg:
			i := &deploy.IxiaTGSpec{}
			path := defaultIxiaTGManifestDir
			if cs.GetIxiatg().ManifestDir != "" {
				path = cs.GetIxiatg().ManifestDir
			}
			p, err := validatePath(path)
			if err != nil {
				return nil, fmt.Errorf("failed to validate path %q", path)
			}
			i.ManifestDir = p
			if cs.GetIxiatg().ConfigMap != nil {
				i.ConfigMap = &deploy.IxiaTGConfigMap{
					Release: cs.GetIxiatg().GetConfigMap().Release,
					Images:  []*deploy.IxiaTGImage{},
				}
				for _, image := range cs.GetIxiatg().GetConfigMap().Images {
					i.ConfigMap.Images = append(i.ConfigMap.Images, &deploy.IxiaTGImage{
						Name: image.Name,
						Path: image.Path,
						Tag:  image.Tag,
					})
				}
			}
			d.Controllers = append(d.Controllers, i)
		default:
			return nil, fmt.Errorf("controller type not supported: %T", t)
		}
	}
	return d, nil
}

func (s *server) CreateCluster(ctx context.Context, req *cpb.CreateClusterRequest) (*cpb.CreateClusterResponse, error) {
	log.Infof("Received CreateCluster request: %v", req)
	d, err := newDeployment(req)
	if err != nil {
		return nil, status.Errorf(codes.InvalidArgument, "unable to parse request: %v", err)
	}
	log.Infof("Parsed request into deployment: %v", d)
	s.muDeploy.Lock()
	defer s.muDeploy.Unlock()
	if _, ok := s.deployments[d.Cluster.GetName()]; ok { // if OK
		return nil, status.Errorf(codes.AlreadyExists, "cluster %q already exists", d.Cluster.GetName())
	}
	if err := d.Deploy(ctx, defaultKubeCfg); err != nil {
		return nil, status.Errorf(codes.Internal, "failed to deploy cluster: %v", err)
	}
	s.deployments[d.Cluster.GetName()] = d
	log.Infof("Cluster %q deployed and ready for topology", d.Cluster.GetName())
	resp := &cpb.CreateClusterResponse{
		Name:  d.Cluster.GetName(),
		State: cpb.ClusterState_CLUSTER_STATE_RUNNING,
	}
	return resp, nil
}

func (s *server) DeleteCluster(ctx context.Context, req *cpb.DeleteClusterRequest) (*cpb.DeleteClusterResponse, error) {
	log.Infof("Received DeleteCluster request: %v", req)
	s.muDeploy.Lock()
	defer s.muDeploy.Unlock()
	d, ok := s.deployments[req.GetName()]
	if !ok {
		return nil, status.Errorf(codes.NotFound, "cluster %q not found, can only delete clusters created using TopologyManager", req.GetName())
	}
	if err := d.Delete(); err != nil {
		return nil, status.Errorf(codes.Internal, "failed to delete cluster: %v", err)
	}
	delete(s.deployments, req.GetName())
	log.Infof("Deleted cluster %q", d.Cluster.GetName())
	return &cpb.DeleteClusterResponse{}, nil
}

func (s *server) ShowCluster(ctx context.Context, req *cpb.ShowClusterRequest) (*cpb.ShowClusterResponse, error) {
	log.Infof("Received ShowCluster request: %v", req)
	s.muDeploy.Lock()
	defer s.muDeploy.Unlock()
	d, ok := s.deployments[req.GetName()]
	if !ok {
		return nil, status.Errorf(codes.NotFound, "cluster %q not found, can only show clusters created using TopologyManager", req.GetName())
	}
	if err := d.Healthy(ctx); err != nil {
		return &cpb.ShowClusterResponse{State: cpb.ClusterState_CLUSTER_STATE_ERROR}, nil
	}
	return &cpb.ShowClusterResponse{State: cpb.ClusterState_CLUSTER_STATE_RUNNING}, nil
}

func (s *server) CreateTopology(ctx context.Context, req *cpb.CreateTopologyRequest) (*cpb.CreateTopologyResponse, error) {
	log.Infof("Received CreateTopology request: %v", req)
	topoPb := req.GetTopology()
	if topoPb == nil {
		return nil, status.Errorf(codes.InvalidArgument, "invalid request: missing topology protobuf")
	}
	if topoPb.GetName() == "" {
		return nil, status.Errorf(codes.InvalidArgument, "missing topology name")
	}

	s.muTopo.Lock()
	defer s.muTopo.Unlock()
	if _, ok := s.topos[topoPb.GetName()]; ok {
		return nil, status.Errorf(codes.AlreadyExists, "topology %q already exists", req.Topology.GetName())
	}

	for _, node := range topoPb.Nodes {
		if node.GetConfig() == nil || node.GetConfig().GetFile() == "" {
			// A config section is not required: you are allowed to bring up a
			// topology with no initial config.
			continue
		}
		path := node.GetConfig().GetFile()
		if !filepath.IsAbs(path) {
			path = filepath.Join(defaultTopoBasePath, path)
		}
		log.Infof("Checking config path: %q", path)
		if _, err := validatePath(path); err != nil {
			return nil, status.Errorf(codes.InvalidArgument, "config file not found for node %q: %v", node.GetName(), err)
		}
		node.GetConfig().ConfigData = &tpb.Config_File{File: path}
	}
	// Saves the original topology protobuf.
	txtPb, err := prototext.Marshal(topoPb)
	if err != nil {
		return nil, status.Errorf(codes.Internal, "invalid topology protobuf: %v", err)
	}
	path := defaultKubeCfg
	if req.Kubecfg != "" {
		path = req.Kubecfg
	}
	kcfg, err := validatePath(path)
	if err != nil {
		return nil, status.Errorf(codes.InvalidArgument, "kubecfg %q does not exist: %v", path, err)
	}
	tm, err := topo.New(topoPb, topo.WithKubecfg(kcfg))
	if err != nil {
		return nil, status.Errorf(codes.Internal, "failed to create topology manager: %v", err)
	}
	if err := tm.Create(ctx, 0); err != nil {
		return nil, status.Errorf(codes.Internal, "failed to create topology: %v", err)
	}

	s.topos[topoPb.GetName()] = txtPb
	return &cpb.CreateTopologyResponse{
		TopologyName: req.Topology.GetName(),
		State:        cpb.TopologyState_TOPOLOGY_STATE_RUNNING,
	}, nil
}

func (s *server) DeleteTopology(ctx context.Context, req *cpb.DeleteTopologyRequest) (*cpb.DeleteTopologyResponse, error) {
	log.Infof("Received DeleteTopology request: %v", req)
	s.muTopo.Lock()
	defer s.muTopo.Unlock()
	txtPb, ok := s.topos[req.GetTopologyName()]
	if !ok {
		return nil, status.Errorf(codes.NotFound, "topology %q not found", req.GetTopologyName())
	}
	topoPb := &tpb.Topology{}
	if err := prototext.Unmarshal(txtPb, topoPb); err != nil {
		return nil, status.Errorf(codes.Internal, "invalid topology protobuf: %v", err)
	}
	kcfg, err := validatePath(defaultKubeCfg)
	if err != nil {
		return nil, status.Errorf(codes.Internal, "default kubecfg %q does not exist: %v", defaultKubeCfg, err)
	}
	tm, err := topo.New(topoPb, topo.WithKubecfg(kcfg))
	if err != nil {
		return nil, status.Errorf(codes.Internal, "failed to create topology manager: %v", err)
	}
	if err := tm.Delete(ctx); err != nil {
		return nil, status.Errorf(codes.Internal, "failed to delete topology: %v", err)
	}
	return &cpb.DeleteTopologyResponse{}, nil
}

func (s *server) ShowTopology(ctx context.Context, req *cpb.ShowTopologyRequest) (*cpb.ShowTopologyResponse, error) {
	log.Infof("Received ShowTopology request: %v", req)
	s.muTopo.Lock()
	defer s.muTopo.Unlock()
	txtPb, ok := s.topos[req.GetTopologyName()]
	if !ok {
		return nil, status.Errorf(codes.NotFound, "topology %q not found", req.GetTopologyName())
	}
	topoPb := &tpb.Topology{}
	if err := prototext.Unmarshal(txtPb, topoPb); err != nil {
		return nil, status.Errorf(codes.Internal, "invalid topology protobuf: %v", err)
	}
	kcfg, err := validatePath(defaultKubeCfg)
	if err != nil {
		return nil, status.Errorf(codes.Internal, "default kubecfg %q does not exist: %v", defaultKubeCfg, err)
	}
	tm, err := topo.New(topoPb, topo.WithKubecfg(kcfg))
	if err != nil {
		return nil, status.Errorf(codes.Internal, "failed to create topology manager: %v", err)
	}
	resp, err := tm.Show(ctx)
	if err != nil {
		return nil, status.Errorf(codes.Internal, "failed to show topology: %v", err)
	}
	return resp, nil
}

func (s *server) PushConfig(ctx context.Context, req *cpb.PushConfigRequest) (*cpb.PushConfigResponse, error) {
	log.Infof("Received PushConfig request: %v", req)
	s.muTopo.Lock()
	defer s.muTopo.Unlock()
	txtPb, ok := s.topos[req.GetTopologyName()]
	if !ok {
		return nil, status.Errorf(codes.NotFound, "topology %q not found", req.GetTopologyName())
	}
	topoPb := &tpb.Topology{}
	if err := prototext.Unmarshal(txtPb, topoPb); err != nil {
		return nil, status.Errorf(codes.Internal, "invalid topology protobuf: %v", err)
	}
	kcfg, err := validatePath(defaultKubeCfg)
	if err != nil {
		return nil, status.Errorf(codes.Internal, "default kubecfg %q does not exist: %v", defaultKubeCfg, err)
	}
	tm, err := topo.New(topoPb, topo.WithKubecfg(kcfg))
	if err != nil {
		return nil, status.Errorf(codes.Internal, "failed to create topology manager for %s: %v", topoPb.Name, err)
	}
	log.Infof("Pushing config of size %v to device %q", len(req.GetConfig()), req.GetDeviceName())
	if err := tm.ConfigPush(ctx, req.GetDeviceName(), bytes.NewReader(req.GetConfig())); err != nil {
		return nil, status.Errorf(codes.Internal, "failed to push config to device %q: %v", req.GetDeviceName(), err)
	}
	return &cpb.PushConfigResponse{}, nil
}

func (s *server) ResetConfig(ctx context.Context, req *cpb.ResetConfigRequest) (*cpb.ResetConfigResponse, error) {
	log.Infof("Received ResetConfig request: %v", req)
	s.muTopo.Lock()
	defer s.muTopo.Unlock()
	txtPb, ok := s.topos[req.GetTopologyName()]
	if !ok {
		return nil, status.Errorf(codes.NotFound, "topology %q not found", req.GetTopologyName())
	}
	topoPb := &tpb.Topology{}
	if err := prototext.Unmarshal(txtPb, topoPb); err != nil {
		return nil, status.Errorf(codes.Internal, "invalid topology protobuf: %v", err)
	}
	kcfg, err := validatePath(defaultKubeCfg)
	if err != nil {
		return nil, status.Errorf(codes.Internal, "default kubecfg %q does not exist: %v", defaultKubeCfg, err)
	}
	tm, err := topo.New(topoPb, topo.WithKubecfg(kcfg))
	if err != nil {
		return nil, status.Errorf(codes.Internal, "failed to create topology manager for %s: %v", topoPb.Name, err)
	}
	log.Infof("Resetting config for device %q", req.GetDeviceName())
	if err := tm.ResetCfg(ctx, req.GetDeviceName()); err != nil {
		return nil, status.Errorf(codes.Internal, "failed to reset config for device %q: %v", req.GetDeviceName(), err)
	}
	return &cpb.ResetConfigResponse{}, nil
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
	s := grpc.NewServer(
		grpc.Creds(creds),
		grpc.KeepaliveEnforcementPolicy(keepalive.EnforcementPolicy{
			PermitWithoutStream: true,
			MinTime:             time.Second * 10,
		}),
	)
	cpb.RegisterTopologyManagerServer(s, newServer())
	log.Infof("Controller server listening at %v", lis.Addr())
	if err := s.Serve(lis); err != nil {
		log.Fatalf("failed to serve: %v", err)
	}
}
