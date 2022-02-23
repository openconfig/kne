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
package topology

import (
	"bytes"
	"context"
	"fmt"
	"io/ioutil"
	"os"
	"path/filepath"

	tpb "github.com/google/kne/proto/topo"
	"github.com/google/kne/topo"
	"github.com/google/kne/topo/node"
	"github.com/openconfig/gnmi/errlist"
	log "github.com/sirupsen/logrus"
	"github.com/spf13/cobra"
	"google.golang.org/protobuf/encoding/prototext"
)

func New() *cobra.Command {
	pushCmd := &cobra.Command{
		Use:   "push <topology> <device> <config file>",
		Short: "push config to device",
		RunE:  pushFn,
	}
	watchCmd := &cobra.Command{
		Use:   "watch <topology>",
		Short: "watch will watch the current topologies",
		RunE:  watchFn,
	}
	serviceCmd := &cobra.Command{
		Use:   "service <topology>",
		Short: "service returns the current topology with service endpoints defined.",
		RunE:  serviceFn,
	}
	certCmd := &cobra.Command{
		Use:   "cert <topology> <device>",
		Short: "push or generate certs for nodes in topology",
		RunE:  certFn,
	}
	resetCfgCmd := &cobra.Command{
		Use:   "reset <topology> <device>",
		Short: "reset configuration of device to vendor default (if device not provide reset all nodes)",
		RunE:  resetCfgFn,
	}
	topoCmd := &cobra.Command{
		Use:   "topology",
		Short: "Topology commands.",
	}
	topoCmd.AddCommand(certCmd)
	topoCmd.AddCommand(pushCmd)
	topoCmd.AddCommand(serviceCmd)
	topoCmd.AddCommand(watchCmd)
	resetCfgCmd.Flags().BoolVar(&skipReset, "skip", skipReset, "skip nodes if they are not resetable")
	resetCfgCmd.Flags().BoolVar(&pushConfig, "push", pushConfig, "additionally push orginal topology configuration")
	topoCmd.AddCommand(resetCfgCmd)
	return topoCmd
}

var (
	skipReset  bool
	pushConfig bool
)

func fileRelative(p string) (string, error) {
	bp, err := filepath.Abs(p)
	if err != nil {
		return "", err
	}
	return filepath.Dir(bp), nil
}

var (
	opts []topo.Option
)

func resetCfgFn(cmd *cobra.Command, args []string) error {
	if len(args) < 1 || len(args) > 2 {
		return fmt.Errorf("%s: invalid args", cmd.Use)
	}
	topopb, err := topo.Load(args[0])
	if err != nil {
		return fmt.Errorf("%s: %w", cmd.Use, err)
	}
	s, err := cmd.Flags().GetString("kubecfg")
	if err != nil {
		return err
	}
	t, err := topo.New(s, topopb, opts...)
	if err != nil {
		return fmt.Errorf("%s: %w", cmd.Use, err)
	}
	ctx := cmd.Context()
	t.Load(ctx)
	nodes := t.Nodes()
	if len(args) > 1 {
		n, err := t.Node(args[1])
		if err != nil {
			return err
		}
		nodes = []node.Node{n}
	}
	var canReset []node.Node
	for _, n := range nodes {
		_, ok := n.(node.Resetter)
		if !ok {
			if skipReset {
				continue
			}
			return fmt.Errorf("node %s is not resettable and --skip not set", n.Name())
		}
		canReset = append(canReset, n)
	}
	for _, r := range canReset {
		if err := r.(node.Resetter).ResetCfg(ctx); err != nil {
			return err
		}
	}
	if !pushConfig {
		log.Infof("Finished reseting resetable nodes to vendor default configuration")
		return nil
	}
	log.Infof("Trying to repush devices configs: %q", args[0])
	bp, err := fileRelative(args[0])
	if err != nil {
		return fmt.Errorf("failed to find relative path for topology: %v", err)
	}
	var errList errlist.List
	for _, n := range canReset {
		cd := n.GetProto().GetConfig().GetConfigData()
		if cd == nil {
			log.Infof("Skipping node %q no config provided", n.Name())
			continue
		}
		cp, ok := n.(node.ConfigPusher)
		if !ok {
			log.Infof("Skipping node %q not a ConfigPusher", n.Name())
			continue
		}
		var b []byte
		switch v := cd.(type) {
		case *tpb.Config_Data:
			b = v.Data
		case *tpb.Config_File:
			cPath := v.File
			if !filepath.IsAbs(cPath) {
				cPath = filepath.Join(bp, cPath)
			}
			log.Infof("Pushing configuration %q to %q", cPath, n.Name())
			var err error
			b, err = ioutil.ReadFile(cPath)
			if err != nil {
				errList.Add(err)
				continue
			}
		}
		if err := cp.ConfigPush(context.Background(), bytes.NewBuffer(b)); err != nil {
			errList.Add(err)
		}
	}
	return errList.Err()
}

func pushFn(cmd *cobra.Command, args []string) error {
	if len(args) != 3 {
		return fmt.Errorf("%s: invalid args", cmd.Use)
	}
	topopb, err := topo.Load(args[0])
	if err != nil {
		return fmt.Errorf("%s: %w", cmd.Use, err)
	}
	s, err := cmd.Flags().GetString("kubecfg")
	if err != nil {
		return err
	}
	t, err := topo.New(s, topopb, opts...)
	if err != nil {
		return fmt.Errorf("%s: %w", cmd.Use, err)
	}

	ctx := cmd.Context()
	t.Load(ctx)
	fp, err := os.Open(args[2])
	if err != nil {
		return err
	}
	defer func() {
		if err := fp.Close(); err != nil {
			log.Warnf("failed to close config file %q", args[2])
		}
	}()
	if err := t.ConfigPush(ctx, args[1], fp); err != nil {
		return err
	}
	return nil
}

func watchFn(cmd *cobra.Command, args []string) error {
	if len(args) != 1 {
		return fmt.Errorf("%s: missing topology", cmd.Use)
	}
	topopb, err := topo.Load(args[0])
	if err != nil {
		return fmt.Errorf("%s: %w", cmd.Use, err)
	}
	s, err := cmd.Flags().GetString("kubecfg")
	if err != nil {
		return err
	}
	t, err := topo.New(s, topopb)
	if err != nil {
		return fmt.Errorf("%s: %w", cmd.Use, err)
	}
	if err := t.Watch(cmd.Context()); err != nil {
		return err
	}
	return nil
}

func certFn(cmd *cobra.Command, args []string) error {
	if len(args) != 2 {
		return fmt.Errorf("%s: missing args", cmd.Use)
	}
	topopb, err := topo.Load(args[0])
	if err != nil {
		return fmt.Errorf("%s: %w", cmd.Use, err)
	}
	s, err := cmd.Flags().GetString("kubecfg")
	if err != nil {
		return err
	}
	t, err := topo.New(s, topopb)
	if err != nil {
		return fmt.Errorf("%s: %w", cmd.Use, err)
	}
	t.Load(cmd.Context())
	if err != nil {
		return fmt.Errorf("%s: %w", cmd.Use, err)
	}
	n, err := t.Node(args[1])
	if err != nil {
		return err
	}
	return topo.GenerateSelfSigned(cmd.Context(), n)
}

var (
	topoNew = defaultNewTopo
)

type resourcer interface {
	Load(context.Context) error
	Resources(context.Context) (*topo.Resources, error)
}

func defaultNewTopo(kubeCfg string, t *tpb.Topology, opts ...topo.Option) (resourcer, error) {
	return topo.New(kubeCfg, t, opts...)
}

var (
	getTopologyServices = topo.GetTopologyServices
)

func serviceFn(cmd *cobra.Command, args []string) error {
	if len(args) != 1 {
		return fmt.Errorf("%s: missing topology", cmd.Use)
	}
	kubeCfg, err := cmd.Flags().GetString("kubecfg")
	if err != nil {
		return err
	}
	param := topo.TopologyParams{
		TopoName: args[0],
		Kubecfg:  kubeCfg,
	}
	ts, err := getTopologyServices(cmd.Context(), param)
	if err != nil {
		return err
	}

	fmt.Fprintln(cmd.OutOrStdout(), prototext.Format(ts.Topology))
	return nil
}
