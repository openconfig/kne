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

	"github.com/google/kne/deploy"

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

func NewDeployment(cfg *deploy.DeploymentConfig) (*deploy.Deployment, error) {
	d := &deploy.Deployment{}
	switch cfg.Cluster.Kind {
	case "Kind":
		log.Infof("Using kind scenario")
		v := &deploy.KindSpec{}
		if err := cfg.Cluster.Spec.Decode(v); err != nil {
			return nil, err
		}
		d.Cluster = v
	default:
		return nil, fmt.Errorf("cluster type not supported: %s", cfg.Cluster.Kind)
	}
	switch cfg.CNI.Kind {
	case "Meshnet":
		v := &deploy.MeshnetSpec{}
		if err := cfg.CNI.Spec.Decode(v); err != nil {
			return nil, err
		}
		var err error
		fmt.Println("v.ManifestDir Meshnet: ", v.ManifestDir)
		v.ManifestDir, err = filepath.Abs(v.ManifestDir)
		if err != nil {
			return nil, err

		}
		d.CNI = v
	default:
		return nil, fmt.Errorf("CNI type not supported: %s", cfg.CNI.Kind)
	}
	switch cfg.Ingress.Kind {
	case "MetalLB":
		v := &deploy.MetalLBSpec{}
		if err := cfg.Ingress.Spec.Decode(v); err != nil {
			return nil, err
		}
		fmt.Println("v.ManifestDir MetalLB: ", v.ManifestDir)
		var err error
		v.ManifestDir, err = filepath.Abs(v.ManifestDir)
		if err != nil {
			return nil, err

		}
		log.Infof("v.ManifestDir: %s", v.ManifestDir)
		d.Ingress = v
	default:
		return nil, fmt.Errorf("ingress type not supported: %s", cfg.Ingress.Kind)
	}
	return d, nil
}

var (
	deploymentBasePath string
)

func deploymentFromArg(p string) (*deploy.DeploymentConfig, string, error) {
	bp, err := filepath.Abs(p)
	if err != nil {
		return nil, "", err
	}
	b, err := ioutil.ReadFile(bp)
	if err != nil {
		return nil, "", err
	}
	bp = filepath.Dir(bp)
	dCfg := &deploy.DeploymentConfig{}
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
	log.Info(d.Ingress)
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
