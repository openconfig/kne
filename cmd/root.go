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
	"context"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/google/kne/cmd/deploy"
	"github.com/google/kne/cmd/topology"
	"github.com/google/kne/topo"
	"github.com/kr/pretty"
	log "github.com/sirupsen/logrus"
	"github.com/spf13/cobra"
	"k8s.io/client-go/util/homedir"
)

var (
	defaultKubeCfg = ""
	kubecfg        string
	topofile       string
	dryrun         bool
	timeout        time.Duration
	logLevel       = "info"

	rootCmd = &cobra.Command{
		Use:   "kne_cli",
		Short: "Kubernetes Network Emulation CLI",
		Long: `Kubernetes Network Emulation CLI.  Works with meshnet to create 
layer 2 topology used by containers to layout networks in a k8s
environment.`,
		SilenceUsage:      true,
		PersistentPreRunE: rootFn,
	}
)

func rootFn(cmd *cobra.Command, args []string) error {
	l, err := log.ParseLevel(logLevel)
	if err != nil {
		return err
	}
	log.SetLevel(l)
	return nil
}

// ExecuteContext executes the root command.
func ExecuteContext(ctx context.Context) error {
	return rootCmd.ExecuteContext(ctx)
}

func init() {
	if home := homedir.HomeDir(); home != "" {
		defaultKubeCfg = filepath.Join(home, ".kube", "config")
	}
	rootCmd.SetOut(os.Stdout)
	rootCmd.PersistentFlags().StringVar(&kubecfg, "kubecfg", defaultKubeCfg, "kubeconfig file")
	rootCmd.PersistentFlags().StringVarP(&logLevel, "verbosity", "v", logLevel, "log level")
	createCmd.Flags().BoolVar(&dryrun, "dryrun", false, "Generate topology but do not push to k8s")
	createCmd.Flags().DurationVar(&timeout, "timeout", 0, "Timeout for pod status enquiry")
	rootCmd.AddCommand(createCmd)
	rootCmd.AddCommand(deleteCmd)
	rootCmd.AddCommand(showCmd)
	rootCmd.AddCommand(topology.New())
	rootCmd.AddCommand(deploy.New())
	//rootCmd.AddCommand(graphCmd)
}

var (
	createCmd = &cobra.Command{
		Use:       "create <topology file>",
		Short:     "Create Topology",
		PreRunE:   validateTopology,
		RunE:      createFn,
		ValidArgs: []string{"topology"},
	}
	deleteCmd = &cobra.Command{
		Use:       "delete <topology file>",
		Short:     "Delete Topology",
		PreRunE:   validateTopology,
		RunE:      deleteFn,
		ValidArgs: []string{"topology"},
	}
	showCmd = &cobra.Command{
		Use:       "show <topology file>",
		Short:     "Show Topology",
		PreRunE:   validateTopology,
		RunE:      showFn,
		ValidArgs: []string{"topology"},
	}
)

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
	p := topo.TopologyParams{
		TopoName:       args[0],
		Kubecfg:        kubecfg,
		TopoNewOptions: []topo.Option{topo.WithBasePath(bp)},
		Timeout:        timeout,
		DryRun:         dryrun,
	}
	return topo.CreateTopology(cmd.Context(), p)
}

func deleteFn(cmd *cobra.Command, args []string) error {
	p := topo.TopologyParams{
		TopoName: args[0],
		Kubecfg:  kubecfg,
	}
	return topo.DeleteTopology(cmd.Context(), p)
}

func showFn(cmd *cobra.Command, args []string) error {
	topopb, err := topo.Load(args[0])
	if err != nil {
		return fmt.Errorf("%s: %w", cmd.Use, err)
	}
	t, err := topo.New(kubecfg, topopb)
	if err != nil {
		return fmt.Errorf("%s: %w", cmd.Use, err)
	}
	if err := t.Load(cmd.Context()); err != nil {
		return err
	}
	out := cmd.OutOrStdout()
	r, err := t.Resources(cmd.Context())
	if err != nil {
		return err
	}
	fmt.Fprintf(out, "Pods:\n")
	for _, pods := range r.Pods {
		for _, p := range pods {
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
