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

package cmd

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/kr/pretty"
	"github.com/openconfig/kne/cmd/deploy"
	"github.com/openconfig/kne/cmd/topology"
	"github.com/openconfig/kne/topo"
	"github.com/spf13/cobra"
	"github.com/spf13/viper"
	"k8s.io/client-go/util/homedir"
	log "k8s.io/klog/v2"
)

func New() *cobra.Command {
	root := &cobra.Command{
		Use:   "kne",
		Short: "Kubernetes Network Emulation CLI",
		Long: `Kubernetes Network Emulation CLI.  Works with meshnet to create
layer 2 topology used by containers to layout networks in a k8s
environment.`,
		SilenceUsage: true,
	}
	root.SetOut(os.Stdout)
	cfgFile := root.PersistentFlags().String("config_file", defaultCfgFile(), "Path to KNE config file")
	root.PersistentFlags().String("kubecfg", defaultKubeCfg(), "kubeconfig file")
	root.PersistentFlags().Bool("report_usage", false, "Whether to reporting anonymous usage metrics")
	root.PersistentFlags().String("report_usage_project_id", "", "Project to report anonymous usage metrics to")
	root.PersistentFlags().String("report_usage_topic_id", "", "Topic to report anonymous usage metrics to")
	root.PersistentPreRunE = func(cmd *cobra.Command, args []string) error {
		if *cfgFile == "" {
			return nil
		}
		if _, err := os.Stat(*cfgFile); err == nil {
			viper.SetConfigFile(*cfgFile)
			if err := viper.ReadInConfig(); err != nil {
				return fmt.Errorf("error reading config: %w", err)
			}
		}
		viper.BindPFlags(cmd.Flags())
		viper.SetDefault("report_usage", false)
		return nil
	}
	root.AddCommand(newCreateCmd())
	root.AddCommand(newDeleteCmd())
	root.AddCommand(newShowCmd())
	root.AddCommand(topology.New())
	root.AddCommand(deploy.New())
	return root
}

func defaultCfgFile() string {
	if home := homedir.HomeDir(); home != "" {
		return filepath.Join(home, ".config", "kne", "config.yaml")
	}
	return ""
}

func defaultKubeCfg() string {
	if v := os.Getenv("KUBECONFIG"); v != "" {
		return v
	}
	if home := homedir.HomeDir(); home != "" {
		return filepath.Join(home, ".kube", "config")
	}
	return ""
}

func newCreateCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:       "create <topology file>",
		Short:     "Create Topology",
		PreRunE:   validateTopology,
		RunE:      createFn,
		ValidArgs: []string{"topology"},
	}
	cmd.Flags().Bool("dryrun", false, "Generate topology but do not push to k8s")
	cmd.Flags().Duration("timeout", 0, "Timeout for pod status enquiry")
	cmd.Flags().Bool("progress", false, "Display progress of container bringup")
	return cmd
}

func newDeleteCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:       "delete <topology file>",
		Short:     "Delete Topology",
		PreRunE:   validateTopology,
		RunE:      deleteFn,
		ValidArgs: []string{"topology"},
	}
	return cmd
}

func newShowCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:       "show <topology file>",
		Short:     "Show Topology",
		PreRunE:   validateTopology,
		RunE:      showFn,
		ValidArgs: []string{"topology"},
	}
	return cmd
}

func validateTopology(cmd *cobra.Command, args []string) error {
	if len(args) == 0 {
		return fmt.Errorf("%s: topology must be provided", cmd.Use)
	}
	return nil
}

func fileRelative(p string) (string, error) {
	bp, err := filepath.Abs(p)
	if err != nil {
		return "", err
	}
	return filepath.Dir(bp), nil
}

func createFn(cmd *cobra.Command, args []string) error {
	bp, err := fileRelative(args[0])
	if err != nil {
		return err
	}
	log.Infof(bp)
	topopb, err := topo.Load(args[0])
	if err != nil {
		return fmt.Errorf("%s: %w", cmd.Use, err)
	}
	opts := []topo.Option{
		topo.WithKubecfg(viper.GetString("kubecfg")),
		topo.WithBasePath(bp),
		topo.WithProgress(viper.GetBool("progress")),
		topo.WithUsageReporting(
			viper.GetBool("report_usage"),
			viper.GetString("report_usage_project_id"),
			viper.GetString("report_usage_topic_id"),
		),
	}
	tm, err := topo.New(topopb, opts...)
	if err != nil {
		return fmt.Errorf("%s: %w", cmd.Use, err)
	}
	if viper.GetBool("dryrun") {
		return nil
	}
	return tm.Create(cmd.Context(), viper.GetDuration("timeout"))
}

func deleteFn(cmd *cobra.Command, args []string) error {
	topopb, err := topo.Load(args[0])
	if err != nil {
		return fmt.Errorf("%s: %w", cmd.Use, err)
	}
	tm, err := topo.New(topopb, topo.WithKubecfg(viper.GetString("kubecfg")))
	if err != nil {
		return fmt.Errorf("%s: %w", cmd.Use, err)
	}
	return tm.Delete(cmd.Context())
}

func showFn(cmd *cobra.Command, args []string) error {
	topopb, err := topo.Load(args[0])
	if err != nil {
		return fmt.Errorf("%s: %w", cmd.Use, err)
	}
	tm, err := topo.New(topopb, topo.WithKubecfg(viper.GetString("kubecfg")))
	if err != nil {
		return fmt.Errorf("%s: %w", cmd.Use, err)
	}
	out := cmd.OutOrStdout()
	r, err := tm.Resources(cmd.Context())
	if err != nil {
		return err
	}
	fmt.Fprintf(out, "Pods:\n")
	for k, pods := range r.Pods {
		fmt.Fprintf(out, "Pod %s:\n", k)
		for _, p := range pods {
			if p == nil {
				fmt.Fprintf(out, "pod in unknown state: nil\n")
				continue
			}
			fmt.Fprintf(out, "%s:%s IP:%s\n", topopb.Name, p.Name, p.Status.PodIP)
		}
	}
	fmt.Fprintf(out, "Topologies:\n")
	for _, p := range r.Topologies {
		fmt.Fprintf(out, "%s:%s\nSpec:\n%s\nStatus:\n%s\n", topopb.Name, p.Name, pretty.Sprint(p.Spec), pretty.Sprint(p.Status))
	}
	fmt.Fprintf(out, "Services:\n")
	for _, svcs := range r.Services {
		for _, s := range svcs {
			fmt.Fprintf(out, "%s:%s\nSpec:\n%s\nStatus:\n%s\n", topopb.Name, s.Name, pretty.Sprint(s.Spec), pretty.Sprint(s.Status))
		}
	}
	return nil
}
