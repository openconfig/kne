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
	"fmt"
	"os/exec"

	"github.com/openconfig/kne/deploy"
	"github.com/openconfig/kne/load"
	"github.com/spf13/cobra"
	"gopkg.in/yaml.v3"
	log "k8s.io/klog/v2"
)

var progress bool

func New() *cobra.Command {
	deployCmd := &cobra.Command{
		Use:   "deploy <deployment yaml>",
		Short: "Deploy cluster.",
		RunE:  deployFn,
	}
	deployCmd.Flags().BoolVar(&progress, "progress", false, "Display progress of container bringup")
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

// newDeployment reads in a deployment config file and returns a
// deploy.Deployment or an error.  If the testing flag is true the no errors
// will be reported for missing files.
func newDeployment(cfgPath string, testing bool) (*deploy.Deployment, error) {
	c, err := load.NewConfig(cfgPath, &DeploymentConfig{})
	if err != nil {
		return nil, err
	}
	c.IgnoreMissingFiles = testing

	cfg := deploy.Deployment{
		Progress: progress,
	}
	if err := c.Decode(&cfg); err != nil {
		return nil, err
	}
	if cfg.Cluster == nil {
		return nil, fmt.Errorf("Cluster not specified")
	}
	if cfg.Ingress == nil {
		return nil, fmt.Errorf("Ingress not specified")
	}
	if cfg.CNI == nil {
		return nil, fmt.Errorf("CNI not specified")
	}
	if len(cfg.Controllers) == 0 {
		log.Infof("no controllers specified")
	}
	return &cfg, nil
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
	d, err := newDeployment(args[0], false)
	if err != nil {
		return err
	}
	if err := d.Deploy(cmd.Context(), kubecfg); err != nil {
		return err
	}
	log.Infof("Deployment complete, ready for topology")
	return nil
}
