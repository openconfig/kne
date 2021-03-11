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

	"github.com/golang/protobuf/proto"
	"github.com/hfam/kne/topo"
	"github.com/spf13/cobra"
	"k8s.io/client-go/util/homedir"
)

var (
	defaultKubeCfg = ""
	kubecfg        string
	topofile       string
	dryrun         bool

	rootCmd = &cobra.Command{
		Use:   "kne_cli",
		Short: "Kubernetes Network Emulation CLI",
		Long: `Kubernetes Network Emulation CLI.  Works with meshnet to create 
layer 2 topology used by containers to layout networks in a k8s
environment.`,
		SilenceUsage: true,
	}
)

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
	createCmd.Flags().BoolVar(&dryrun, "dryrun", false, "Generate topology but do not push to k8s")
	rootCmd.AddCommand(createCmd)
	rootCmd.AddCommand(deleteCmd)
	rootCmd.AddCommand(showCmd)
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

func createFn(cmd *cobra.Command, args []string) error {
	topopb, err := topo.Load(args[0])
	if err != nil {
		return fmt.Errorf("%s: %w", cmd.Use, err)
	}
	t, err := topo.New(kubecfg, topopb)
	if err != nil {
		return fmt.Errorf("%s: %w", cmd.Use, err)
	}
	out := cmd.OutOrStdout()
	fmt.Fprintf(out, "Topology:\n%s\n", proto.MarshalTextString(topopb))
	if err := t.Load(cmd.Context()); err != nil {
		return fmt.Errorf("failed to load topology: %w", err)
	}
	if dryrun {
		return nil
	}
	return t.Push(cmd.Context())
}

func deleteFn(cmd *cobra.Command, args []string) error {
	topopb, err := topo.Load(args[0])
	if err != nil {
		return fmt.Errorf("%s: %w", cmd.Use, err)
	}
	t, err := topo.New(kubecfg, topopb)
	if err != nil {
		return fmt.Errorf("%s: %w", cmd.Use, err)
	}
	out := cmd.OutOrStdout()
	fmt.Fprintf(out, "Topology:\n%s\n", proto.MarshalTextString(topopb))
	if err := t.Load(cmd.Context()); err != nil {
		return fmt.Errorf("failed to load topology: %w", err)
	}
	if dryrun {
		return nil
	}
	return t.Delete(cmd.Context())
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
	return t.Topology(cmd.Context())
}
