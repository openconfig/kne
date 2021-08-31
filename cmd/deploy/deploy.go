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
	"bytes"
	"context"
	"fmt"
	"io/ioutil"
	"os/exec"
	"path/filepath"
	"time"

	log "github.com/sirupsen/logrus"
	"github.com/spf13/cobra"
	"gopkg.in/yaml.v3"

	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/tools/clientcmd"
)

func New() *cobra.Command {
	deployCmd := &cobra.Command{
		Use:   "deploy <deployment yaml>",
		Short: "Deploy cluster.",
		RunE:  deployFn,
	}
	return deployCmd
}

type Cluster interface {
	Deploy(context.Context) error
}

type Ingress interface {
	Deploy(context.Context) error
	SetKClient(kubernetes.Interface)
	Healthy(context.Context) error
}

type CNI interface {
	Deploy(context.Context) error
	SetKClient(kubernetes.Interface)
	Healthy(context.Context) error
}

type Deployment struct {
	Cluster Cluster
	Ingress Ingress
	CNI     CNI
}

type DeploymentConfig struct {
	Cluster ClusterSpec `yaml:"cluster"`
	Ingress IngressSpec `yaml:"ingress"`
	CNI     CNISpec     `yaml:"cni"`
}

func NewDeployment(cfg *DeploymentConfig) (*Deployment, error) {
	d := &Deployment{}
	switch cfg.Cluster.Kind {
	case "Kind":
		log.Infof("Using kind scenario")
		v := &KindSpec{}
		if err := cfg.Cluster.Spec.Decode(v); err != nil {
			return nil, err
		}
		d.Cluster = v
	default:
		return nil, fmt.Errorf("cluster type not supported: %s", cfg.Cluster.Kind)
	}
	switch cfg.CNI.Kind {
	case "Meshnet":
		v := &MeshnetSpec{}
		if err := cfg.CNI.Spec.Decode(v); err != nil {
			return nil, err
		}
		d.CNI = v
	default:
		return nil, fmt.Errorf("CNI type not supported: %s", cfg.CNI.Kind)
	}
	switch cfg.Ingress.Kind {
	case "MetalLB":
		v := &MetalLBSpec{}
		if err := cfg.Ingress.Spec.Decode(v); err != nil {
			return nil, err
		}
		d.Ingress = v
	default:
		return nil, fmt.Errorf("ingress type not supported: %s", cfg.Ingress.Kind)
	}
	return d, nil
}

var (
	deploymentBasePath string
)

func deploymentFromArg(p string) (*DeploymentConfig, string, error) {
	bp, err := filepath.Abs(p)
	if err != nil {
		return nil, "", err
	}
	b, err := ioutil.ReadFile(bp)
	if err != nil {
		return nil, "", err
	}
	bp = filepath.Dir(bp)
	dCfg := &DeploymentConfig{}
	decoder := yaml.NewDecoder(bytes.NewBuffer(b))
	decoder.KnownFields(true)
	if err := decoder.Decode(dCfg); err != nil {
		return nil, "", err
	}
	return dCfg, bp, nil
}

func deployFn(cmd *cobra.Command, args []string) error {
	if len(args) != 1 {
		return fmt.Errorf("%s: missing args", cmd.Use)
	}
	if _, err := exec.LookPath("kubectl"); err != nil {
		return fmt.Errorf("install kubectl before running deploy: %v", err)
	}
	dCfg, bp, err := deploymentFromArg(args[0])
	if err != nil {
		return err
	}
	deploymentBasePath = bp
	d, err := NewDeployment(dCfg)
	if err != nil {
		return err
	}
	if err := d.Cluster.Deploy(cmd.Context()); err != nil {
		return err
	}
	// Once cluster is up set kClient
	kubecfg, err := cmd.Flags().GetString("kubecfg")
	if err != nil {
		return err
	}
	rCfg, err := clientcmd.BuildConfigFromFlags("", kubecfg)
	if err != nil {
		return err
	}
	kClient, err := kubernetes.NewForConfig(rCfg)
	if err != nil {
		return err
	}
	d.Ingress.SetKClient(kClient)
	log.Infof("Validating cluster health")
	if err := d.Ingress.Deploy(cmd.Context()); err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(cmd.Context(), 1*time.Minute)
	defer cancel()
	if err := d.Ingress.Healthy(ctx); err != nil {
		return err
	}
	if err := d.CNI.Deploy(cmd.Context()); err != nil {
		return err
	}
	d.CNI.SetKClient(kClient)
	ctx, cancel = context.WithTimeout(cmd.Context(), 1*time.Minute)
	defer cancel()
	if err := d.CNI.Healthy(ctx); err != nil {
		return err
	}
	log.Infof("Ready for topology")
	return nil
}
