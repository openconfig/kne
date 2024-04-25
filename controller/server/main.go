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
	"os/exec"
	"path/filepath"
	"regexp"
	"sync"
	"time"

	log "github.com/golang/glog"
	"github.com/openconfig/kne/cluster/kubeadm"
	"github.com/openconfig/kne/deploy"
	"github.com/openconfig/kne/exec/run"
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
	kubeadmTokenRE = regexp.MustCompile(`^kubeadm join ([0-9A-Za-z\.:]+) --token ([0-9A-Za-z\.]+) --discovery-token-ca-cert-hash ([0-9A-Za-z:]+)`)

	defaultKubeCfg                        = ""
	defaultTopoBasePath                   = ""
	defaultKubeadmPodNetworkAddOnManifest = ""
	defaultMeshnetManifest                = ""
	defaultMetalLBManifest                = ""
	defaultIxiaTGOperator                 = ""
	defaultIxiaTGConfigMap                = ""
	defaultSRLinuxOperator                = ""
	defaultCEOSLabOperator                = ""
	defaultLemmingOperator                = ""
	// Flags.
	port                 = flag.Int("port", 50051, "Controller server port")
	reportUsage          = flag.Bool("report_usage", false, "Whether to reporting anonymous usage metrics")
	reportUsageProjectID = flag.String("report_usage_project_id", "", "Project to report anonymous usage metrics to")
	reportUsageTopicID   = flag.String("report_usage_topic_id", "", "Topic to report anonymous usage metrics to")
)

func init() {
	if home := homedir.HomeDir(); home != "" {
		defaultKubeCfg = filepath.Join(home, ".kube", "config")
		defaultTopoBasePath = filepath.Join(home, "kne", "examples")
		defaultKubeadmPodNetworkAddOnManifest = filepath.Join(home, "kne", "manifests", "flannel", "manifest.yaml")
		defaultMeshnetManifest = filepath.Join(home, "kne", "manifests", "meshnet", "grpc", "manifest.yaml")
		defaultMetalLBManifest = filepath.Join(home, "kne", "manifests", "metallb", "manifest.yaml")
		defaultIxiaTGOperator = filepath.Join(home, "kne", "manifests", "keysight", "ixiatg-operator.yaml")
		defaultIxiaTGConfigMap = filepath.Join(home, "kne", "manifests", "keysight", "ixiatg-configmap.yaml")
		defaultSRLinuxOperator = filepath.Join(home, "kne", "manifests", "controllers", "srlinux", "manifest.yaml")
		defaultCEOSLabOperator = filepath.Join(home, "kne", "manifests", "controllers", "ceoslab", "manifest.yaml")
		defaultLemmingOperator = filepath.Join(home, "kne", "manifests", "controllers", "lemming", "manifest.yaml")
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
	case *cpb.CreateClusterRequest_Kubeadm:
		k := &deploy.KubeadmSpec{
			CRISocket:                   req.GetKubeadm().CriSocket,
			PodNetworkCIDR:              req.GetKubeadm().PodNetworkCidr,
			TokenTTL:                    req.GetKubeadm().TokenTtl,
			Network:                     req.GetKubeadm().Network,
			AllowControlPlaneScheduling: req.GetKubeadm().AllowControlPlaneScheduling,
		}
		switch t := req.GetKubeadm().GetPodNetworkAddOnManifest().GetManifestData().(type) {
		case *cpb.Manifest_Data:
			k.PodNetworkAddOnManifestData = req.GetKubeadm().GetPodNetworkAddOnManifest().GetData()
		case *cpb.Manifest_File, nil: // if the manifest field is empty, use the default filepath
			path := defaultKubeadmPodNetworkAddOnManifest
			if req.GetKubeadm().GetPodNetworkAddOnManifest().GetFile() != "" {
				path = req.GetKubeadm().GetPodNetworkAddOnManifest().GetFile()
			}
			p, err := validatePath(path)
			if err != nil {
				return nil, fmt.Errorf("failed to validate path %q", path)
			}
			k.PodNetworkAddOnManifest = p
		default:
			return nil, fmt.Errorf("manifest data type not supported: %T", t)
		}
		d.Cluster = k
	case *cpb.CreateClusterRequest_External:
		d.Cluster = &deploy.ExternalSpec{
			Network: req.GetExternal().Network,
		}
	default:
		return nil, fmt.Errorf("cluster type not supported: %T", t)
	}
	switch t := req.IngressSpec.(type) {
	case *cpb.CreateClusterRequest_Metallb:
		m := &deploy.MetalLBSpec{}
		switch t := req.GetMetallb().GetManifest().GetManifestData().(type) {
		case *cpb.Manifest_Data:
			m.ManifestData = req.GetMetallb().GetManifest().GetData()
		case *cpb.Manifest_File, nil: // if the manifest field is empty, use the default filepath
			path := defaultMetalLBManifest
			if req.GetMetallb().GetManifest().GetFile() != "" {
				path = req.GetMetallb().GetManifest().GetFile()
			}
			p, err := validatePath(path)
			if err != nil {
				return nil, fmt.Errorf("failed to validate path %q", path)
			}
			m.Manifest = p
		default:
			return nil, fmt.Errorf("manifest data type not supported: %T", t)
		}
		m.IPCount = int(req.GetMetallb().IpCount)
		d.Ingress = m
	default:
		return nil, fmt.Errorf("ingress spec not supported: %T", t)
	}
	switch t := req.CniSpec.(type) {
	case *cpb.CreateClusterRequest_Meshnet:
		m := &deploy.MeshnetSpec{}
		switch t := req.GetMeshnet().GetManifest().GetManifestData().(type) {
		case *cpb.Manifest_Data:
			m.ManifestData = req.GetMeshnet().GetManifest().GetData()
		case *cpb.Manifest_File, nil: // if the manifest field is empty, use the default filepath
			path := defaultMeshnetManifest
			if req.GetMeshnet().GetManifest().GetFile() != "" {
				path = req.GetMeshnet().GetManifest().GetFile()
			}
			p, err := validatePath(path)
			if err != nil {
				return nil, fmt.Errorf("failed to validate path %q", path)
			}
			m.Manifest = p
		default:
			return nil, fmt.Errorf("manifest data type not supported: %T", t)
		}
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
			switch t := cs.GetCeoslab().GetOperator().GetManifestData().(type) {
			case *cpb.Manifest_Data:
				c.OperatorData = cs.GetCeoslab().GetOperator().GetData()
			case *cpb.Manifest_File, nil: // if the manifest field is empty, use the default filepath
				path := defaultCEOSLabOperator
				if cs.GetCeoslab().GetOperator().GetFile() != "" {
					path = cs.GetCeoslab().GetOperator().GetFile()
				}
				p, err := validatePath(path)
				if err != nil {
					return nil, fmt.Errorf("failed to validate path %q", path)
				}
				c.Operator = p
			default:
				return nil, fmt.Errorf("manifest data type not supported: %T", t)
			}
			d.Controllers = append(d.Controllers, c)
		case *cpb.ControllerSpec_Srlinux:
			s := &deploy.SRLinuxSpec{}
			switch t := cs.GetSrlinux().GetOperator().GetManifestData().(type) {
			case *cpb.Manifest_Data:
				s.OperatorData = cs.GetSrlinux().GetOperator().GetData()
			case *cpb.Manifest_File, nil: // if the manifest field is empty, use the default filepath
				path := defaultSRLinuxOperator
				if cs.GetSrlinux().GetOperator().GetFile() != "" {
					path = cs.GetSrlinux().GetOperator().GetFile()
				}
				p, err := validatePath(path)
				if err != nil {
					return nil, fmt.Errorf("failed to validate path %q", path)
				}
				s.Operator = p
			default:
				return nil, fmt.Errorf("manifest data type not supported: %T", t)
			}
			d.Controllers = append(d.Controllers, s)
		case *cpb.ControllerSpec_Ixiatg:
			i := &deploy.IxiaTGSpec{}
			switch t := cs.GetIxiatg().GetOperator().GetManifestData().(type) {
			case *cpb.Manifest_Data:
				i.OperatorData = cs.GetIxiatg().GetOperator().GetData()
			case *cpb.Manifest_File, nil: // if the manifest field is empty, use the default filepath
				path := defaultIxiaTGOperator
				if cs.GetIxiatg().GetOperator().GetFile() != "" {
					path = cs.GetIxiatg().GetOperator().GetFile()
				}
				p, err := validatePath(path)
				if err != nil {
					return nil, fmt.Errorf("failed to validate path %q", path)
				}
				i.Operator = p
			default:
				return nil, fmt.Errorf("manifest data type not supported: %T", t)
			}
			switch t := cs.GetIxiatg().GetCfgMap().GetManifestData().(type) {
			case *cpb.Manifest_Data:
				i.ConfigMapData = cs.GetIxiatg().GetCfgMap().GetData()
			case *cpb.Manifest_File, nil: // if the manifest field is empty, use the default filepath
				path := defaultIxiaTGConfigMap
				if cs.GetIxiatg().GetCfgMap().GetFile() != "" {
					path = cs.GetIxiatg().GetCfgMap().GetFile()
				}
				p, err := validatePath(path)
				if err != nil {
					return nil, fmt.Errorf("failed to validate path %q", path)
				}
				i.ConfigMap = p
			default:
				return nil, fmt.Errorf("manifest data type not supported: %T", t)
			}
			d.Controllers = append(d.Controllers, i)
		case *cpb.ControllerSpec_Lemming:
			l := &deploy.LemmingSpec{}
			switch t := cs.GetLemming().GetOperator().GetManifestData().(type) {
			case *cpb.Manifest_Data:
				l.OperatorData = cs.GetLemming().GetOperator().GetData()
			case *cpb.Manifest_File, nil: // if the manifest field is empty, use the default filepath
				path := defaultLemmingOperator
				if cs.GetLemming().GetOperator().GetFile() != "" {
					path = cs.GetLemming().GetOperator().GetFile()
				}
				p, err := validatePath(path)
				if err != nil {
					return nil, fmt.Errorf("failed to validate path %q", path)
				}
				l.Operator = p
			default:
				return nil, fmt.Errorf("manifest data type not supported: %T", t)
			}
			d.Controllers = append(d.Controllers, l)
		default:
			return nil, fmt.Errorf("controller type not supported: %T", t)
		}
	}
	d.Progress = true
	d.ReportUsage = *reportUsage
	d.ReportUsageProjectID = *reportUsageProjectID
	d.ReportUsageTopicID = *reportUsageTopicID
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
	resp := &cpb.ShowClusterResponse{State: cpb.ClusterState_CLUSTER_STATE_RUNNING}
	ks, ok := d.Cluster.(*deploy.KubeadmSpec)
	if !ok {
		return resp, nil
	}
	resp.CriSocket = ks.CRISocket
	if _, err := exec.LookPath("kubeadm"); err != nil {
		return nil, status.Errorf(codes.FailedPrecondition, "install kubeadm to show cluster")
	}
	out, err := run.OutCommand("kubeadm", "token", "create", "--print-join-command")
	if err != nil {
		return nil, status.Errorf(codes.Internal, "failed to get kubeadm cluster token: %v", err)
	}
	matches := kubeadmTokenRE.FindStringSubmatch(string(out))
	if len(matches) != 4 {
		return nil, status.Errorf(codes.Internal, "failed to parse kubeadm cluster token from %q (matches %v)", string(out), matches)
	}
	resp.ApiServerEndpoint = matches[1]
	resp.Token = matches[2]
	resp.DiscoveryTokenCaCertHash = matches[3]
	return resp, nil
}

func (s *server) ApplyCluster(ctx context.Context, req *cpb.ApplyClusterRequest) (*cpb.ApplyClusterResponse, error) {
	log.Infof("Received ApplyCluster request: %v", req)
	s.muDeploy.Lock()
	defer s.muDeploy.Unlock()
	d, ok := s.deployments[req.GetName()]
	if !ok {
		return nil, status.Errorf(codes.NotFound, "cluster %q not found, can only apply config to clusters created using TopologyManager", req.GetName())
	}
	return &cpb.ApplyClusterResponse{}, d.Cluster.Apply(req.GetConfig())
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
	opts := []topo.Option{
		topo.WithKubecfg(kcfg),
		topo.WithUsageReporting(*reportUsage, *reportUsageProjectID, *reportUsageTopicID),
	}
	tm, err := topo.New(topoPb, opts...)
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
	delete(s.topos, req.GetTopologyName())
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

func (s *server) JoinCluster(ctx context.Context, req *cpb.JoinClusterRequest) (*cpb.JoinClusterResponse, error) {
	log.Infof("Received JoinCluster request: %v", req)
	if _, err := exec.LookPath("kubeadm"); err != nil {
		return nil, status.Errorf(codes.FailedPrecondition, "install kubeadm to join")
	}
	args := []string{"kubeadm", "join"}
	if req.GetApiServerEndpoint() == "" {
		return nil, status.Errorf(codes.FailedPrecondition, "api server endpoint required")
	}
	args = append(args, req.GetApiServerEndpoint())
	if req.GetToken() != "" {
		args = append(args, "--token", req.GetToken())
	}
	if req.GetDiscoveryTokenCaCertHash() != "" {
		args = append(args, "--discovery-token-ca-cert-hash", req.GetDiscoveryTokenCaCertHash())
	}
	if req.GetCriSocket() != "" {
		args = append(args, "--cri-socket", req.GetCriSocket())
	}
	if err := run.LogCommand("sudo", args...); err != nil {
		return nil, status.Errorf(codes.Internal, "failed to join kubeadm cluster: %v", err)
	}
	if req.GetCredentialProviderConfig() != "" {
		if err := kubeadm.EnableCredentialProvider(req.GetCredentialProviderConfig()); err != nil {
			return nil, err
		}
	}
	return &cpb.JoinClusterResponse{}, nil
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
