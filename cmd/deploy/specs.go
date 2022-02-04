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
package deploy

import (
	"context"
	"io"
	"time"

	dclient "github.com/docker/docker/client"
	"github.com/google/kne/deploy"
	log "github.com/sirupsen/logrus"
	"gopkg.in/yaml.v3"
	"k8s.io/client-go/kubernetes"
	"sigs.k8s.io/kind/pkg/cluster"
)

type ClusterSpec struct {
	Kind string    `yaml:"kind"`
	Spec yaml.Node `yaml:"spec"`
}

type IngressSpec struct {
	Kind string    `yaml:"kind"`
	Spec yaml.Node `yaml:"spec"`
}

type CNISpec struct {
	Kind string    `yaml:"kind"`
	Spec yaml.Node `yaml:"spec"`
}

type KindSpec struct {
	Name                     string        `yaml:"name"`
	Recycle                  bool          `yaml:"recycle"`
	Version                  string        `yaml:"version"`
	Image                    string        `yaml:"image"`
	Retain                   bool          `yaml:"retain"`
	Wait                     time.Duration `yaml:"wait"`
	Kubecfg                  string        `yaml:"kubecfg"`
	DeployWithClient         bool          `yaml:"deployWithClient"`
	GoogleArtifactRegistries []string      `yaml:"googleArtifactRegistries"`
}

//go:generate mockgen -source=specs.go -destination=mocks/mock_provider.go -package=mocks provider
type provider interface {
	List() ([]string, error)
	Create(name string, options ...cluster.CreateOption) error
}

type execerInterface interface {
	Exec(string, ...string) error
	SetStdout(io.Writer)
	SetStderr(io.Writer)
}

var (
	logOut = log.StandardLogger().Out
)

func (k *KindSpec) Deploy(ctx context.Context) error {
	d := &deploy.KindSpec{}
	return d.Deploy(ctx)
}

type MetalLBSpec struct {
	Version     string `yaml:"version"`
	IPCount     int    `yaml:"ip_count"`
	ManifestDir string `yaml:"manifests"`
	kClient     kubernetes.Interface
	dClient     dclient.NetworkAPIClient
}

func (m *MetalLBSpec) SetKClient(c kubernetes.Interface) {
	m.kClient = c
}

func (m *MetalLBSpec) Deploy(ctx context.Context) error {
	d := &deploy.MetalLBSpec{}
	return d.Deploy(ctx)
}

func (m *MetalLBSpec) Healthy(ctx context.Context) error {
	h := &deploy.MetalLBSpec{}
	return h.Healthy(ctx)
}

type MeshnetSpec struct {
	Image       string `yaml:"image"`
	ManifestDir string `yaml:"manifests"`
	kClient     kubernetes.Interface
}

func (m *MeshnetSpec) SetKClient(c kubernetes.Interface) {
	m.kClient = c
}

func (m *MeshnetSpec) Deploy(ctx context.Context) error {
	d := &deploy.MeshnetSpec{}
	return d.Deploy(ctx)
}

func (m *MeshnetSpec) Healthy(ctx context.Context) error {
	h := &deploy.MeshnetSpec{}
	return h.Healthy(ctx)
}
