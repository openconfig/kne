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
package deploy

import (
	"bytes"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"

	"github.com/openconfig/kne/deploy"
	log "github.com/sirupsen/logrus"
	"github.com/spf13/cobra"
	"gopkg.in/yaml.v3"
)

func New() *cobra.Command {
	deployCmd := &cobra.Command{
		Use:   "deploy <deployment yaml>",
		Short: "Deploy cluster.",
		RunE:  deployFn,
	}
	return deployCmd
}

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

type ControllerSpec struct {
	Kind string    `yaml:"kind"`
	Spec yaml.Node `yaml:"spec"`
}

type DeploymentConfig struct {
	Cluster     ClusterSpec       `yaml:"cluster"`
	Ingress     IngressSpec       `yaml:"ingress"`
	CNI         CNISpec           `yaml:"cni"`
	Controllers []*ControllerSpec `yaml:"controllers"`
}

func newDeployment(cfgPath string) (*deploy.Deployment, error) {
	p, err := filepath.Abs(cfgPath)
	if err != nil {
		return nil, err
	}
	log.Infof("Reading deployment config: %q", p)
	b, err := os.ReadFile(p)
	if err != nil {
		return nil, err
	}
	basePath := filepath.Dir(p)
	cfg := &DeploymentConfig{}
	decoder := yaml.NewDecoder(bytes.NewBuffer(b))
	decoder.KnownFields(true)
	if err := decoder.Decode(cfg); err != nil {
		return nil, err
	}

	d := &deploy.Deployment{}
	switch cfg.Cluster.Kind {
	case "Kind":
		log.Infof("Using kind scenario")
		v := &deploy.KindSpec{}
		if err := cfg.Cluster.Spec.Decode(v); err != nil {
			return nil, err
		}
		// make sure manifests are correct relative to configuration.
		for i, s := range v.AdditionalManifests {
			v.AdditionalManifests[i] = cleanPath(s, basePath)
		}

		// make sure kind config file is relative to configuration.
		if v.KindConfigFile != "" {
			v.KindConfigFile = cleanPath(v.KindConfigFile, basePath)
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
		v.ManifestDir = cleanPath(v.ManifestDir, basePath)
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
		v.ManifestDir = cleanPath(v.ManifestDir, basePath)
		d.Ingress = v
	default:
		return nil, fmt.Errorf("ingress type not supported: %s", cfg.Ingress.Kind)
	}
	if len(cfg.Controllers) != 0 {
		d.Controllers = []deploy.Controller{}
	}
	for _, c := range cfg.Controllers {
		switch c.Kind {
		case "IxiaTG":
			v := &deploy.IxiaTGSpec{}
			if err := c.Spec.Decode(v); err != nil {
				return nil, err
			}
			v.ManifestDir = cleanPath(v.ManifestDir, basePath)
			d.Controllers = append(d.Controllers, v)
		case "SRLinux":
			v := &deploy.SRLinuxSpec{}
			if err := c.Spec.Decode(v); err != nil {
				return nil, err
			}
			v.ManifestDir = cleanPath(v.ManifestDir, basePath)
			d.Controllers = append(d.Controllers, v)
		case "CEOSLab":
			v := &deploy.CEOSLabSpec{}
			if err := c.Spec.Decode(v); err != nil {
				return nil, err
			}
			v.ManifestDir = cleanPath(v.ManifestDir, basePath)
			d.Controllers = append(d.Controllers, v)
		default:
			return nil, fmt.Errorf("controller type not supported: %s", c.Kind)
		}
	}
	return d, nil
}

func cleanPath(path, basePath string) string {
	if filepath.IsAbs(path) {
		return path
	}
	return filepath.Join(basePath, path)
}

func deployFn(cmd *cobra.Command, args []string) error {
	if len(args) != 1 {
		return fmt.Errorf("%s: missing args", cmd.Use)
	}
	if _, err := exec.LookPath("kubectl"); err != nil {
		return fmt.Errorf("install kubectl before running deploy: %v", err)
	}
	kubecfg, err := cmd.Flags().GetString("kubecfg")
	if err != nil {
		return err
	}
	d, err := newDeployment(args[0])
	if err != nil {
		return err
	}
	if err := d.Deploy(cmd.Context(), kubecfg); err != nil {
		return err
	}
	log.Infof("Deployment complete, ready for topology")
	return nil
}
