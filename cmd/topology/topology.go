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
package topology

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/openconfig/gnmi/errlist"
	cpb "github.com/openconfig/kne/proto/controller"
	tpb "github.com/openconfig/kne/proto/topo"
	"github.com/openconfig/kne/topo"
	"github.com/openconfig/kne/topo/node"
	"github.com/spf13/cobra"
	"github.com/spf13/viper"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/encoding/prototext"
	log "k8s.io/klog/v2"
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
	resetCfgCmd.Flags().Bool("skip", false, "skip nodes if they are not resetable")
	resetCfgCmd.Flags().Bool("push", false, "additionally push orginal topology configuration")
	generateCmd := &cobra.Command{
		Use:   "generate",
		Short: "Generates a topology of a given type.",
	}
	ringCmd := &cobra.Command{
		Use:   "ring <topology> <count> <links>",
		Short: "generates a ring topology of count devices using the first node in input topology file with links between each pair of devices",
		Long:  "For example, `generate ring topology.textproto 3 1` will generate a ring: node1[1]->node2[2], node2[1]->node3[1], node3[2]->node1[2]",
		RunE:  generateRingFn,
	}
	generateCmd.AddCommand(ringCmd)

	topoCmd := &cobra.Command{
		Use:   "topology",
		Short: "Topology commands.",
	}
	topoCmd.AddCommand(certCmd)
	topoCmd.AddCommand(pushCmd)
	topoCmd.AddCommand(serviceCmd)
	topoCmd.AddCommand(watchCmd)
	topoCmd.AddCommand(resetCfgCmd)
	topoCmd.AddCommand(generateCmd)
	return topoCmd
}

var (
	opts []topo.Option
)

func fileRelative(p string) (string, error) {
	bp, err := filepath.Abs(p)
	if err != nil {
		return "", err
	}
	return filepath.Dir(bp), nil
}

func resetCfgFn(cmd *cobra.Command, args []string) error {
	if len(args) < 1 || len(args) > 2 {
		return fmt.Errorf("%s: invalid args", cmd.Use)
	}
	topopb, err := topo.Load(args[0])
	if err != nil {
		return fmt.Errorf("%s: %w", cmd.Use, err)
	}
	tOpts := append(opts, topo.WithKubecfg(viper.GetString("kubecfg")))
	tm, err := topo.New(topopb, tOpts...)
	if err != nil {
		return fmt.Errorf("%s: %w", cmd.Use, err)
	}
	nodes := tm.Nodes()
	if len(args) > 1 {
		nodes = map[string]node.Node{args[1]: nodes[args[1]]}
	}
	for name := range nodes {
		err := tm.ResetCfg(cmd.Context(), name)
		switch {
		default:
			return err
		case err == nil:
		case status.Code(err) == codes.Unimplemented && !viper.GetBool("skip"):
			return fmt.Errorf("node %q is not a Resetter and --skip not set", name)
		case status.Code(err) == codes.Unimplemented:
			log.Infof("Skipping node %q not a Resetter", name)
			delete(nodes, name)
			continue
		}
	}
	if !viper.GetBool("push") {
		log.Infof("Finished resetting resettable nodes to vendor default configuration")
		return nil
	}
	log.Infof("Trying to repush devices configs: %q", args[0])
	bp, err := fileRelative(args[0])
	if err != nil {
		return fmt.Errorf("failed to find relative path for topology: %v", err)
	}
	var errList errlist.List
	for name, node := range nodes {
		cd := node.GetProto().GetConfig().GetConfigData()
		if cd == nil {
			log.Infof("Skipping node %q no config provided", name)
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
			log.Infof("Pushing configuration %q to %q", cPath, name)
			var err error
			b, err = os.ReadFile(cPath)
			if err != nil {
				errList.Add(err)
				continue
			}
		}
		err := tm.ConfigPush(cmd.Context(), name, bytes.NewBuffer(b))
		switch {
		default:
			errList.Add(err)
			continue
		case err == nil:
		case status.Code(err) == codes.Unimplemented:
			log.Infof("Skipping node %q not a ConfigPusher", name)
			continue
		}
	}
	return errList.Err()
}

func generateRingFn(cmd *cobra.Command, args []string) error {
	if len(args) != 3 {
		return fmt.Errorf("%s: invalid args", cmd.Use)
	}
	links, err := strconv.Atoi(args[2])
	if err != nil {
		return fmt.Errorf("%s: invalid links: %w", cmd.Use, err)
	}
	if links <= 0 {
		return fmt.Errorf("%s: links must be positive", cmd.Use)
	}
	count, err := strconv.Atoi(args[1])
	if err != nil {
		return fmt.Errorf("%s: invalid count: %w", cmd.Use, err)
	}
	if count <= 1 {
		return fmt.Errorf("%s: count must be greater than 1", cmd.Use)
	}
	topopb, err := topo.Load(args[0])
	if err != nil {
		return fmt.Errorf("%s: %w", cmd.Use, err)
	}
	if len(topopb.GetNodes()) == 0 {
		return fmt.Errorf("%s: no nodes in topology", cmd.Use)
	}
	baseNode := topopb.GetNodes()[0]

	newTopo := &tpb.Topology{
		Name:  topopb.GetName() + "-ring",
		Nodes: []*tpb.Node{},
	}
	// Add nodes
	for i := 0; i < count; i++ {
		n := &tpb.Node{
			Name:     fmt.Sprintf("%s-%d", baseNode.GetName(), i),
			Vendor:   baseNode.GetVendor(),
			Model:    baseNode.GetModel(),
			Os:       baseNode.GetOs(),
			Config:   baseNode.GetConfig(),
			Services: baseNode.GetServices(),
		}
		newTopo.Nodes = append(newTopo.Nodes, n)
	}
	// Add links
	for i := 0; i < count; i++ {
		for j := 1; j <= links; j++ {
			aNode := fmt.Sprintf("%s-%d", baseNode.GetName(), i)
			aInt := fmt.Sprintf("eth%d", j)
			zNodeIndex := (i + 1) % count
			zNode := fmt.Sprintf("%s-%d", baseNode.GetName(), zNodeIndex)
			zInt := fmt.Sprintf("eth%d", j+links)
			newTopo.Links = append(newTopo.Links, &tpb.Link{
				ANode: aNode,
				AInt:  aInt,
				ZNode: zNode,
				ZInt:  zInt,
			})
		}
	}
	ext := filepath.Ext(args[0])
	base := strings.TrimSuffix(args[0], ext)
	outFileName := base + "-output" + ext
	b, err := prototext.MarshalOptions{Multiline: true}.Marshal(newTopo)
	if err != nil {
		return fmt.Errorf("failed to marshal new topology: %w", err)
	}
	if err := os.WriteFile(outFileName, b, 0644); err != nil {
		return fmt.Errorf("failed to write output topology file: %w", err)
	}
	log.Infof("Successfully wrote new topology to %q", outFileName)
	return nil
}

func pushFn(cmd *cobra.Command, args []string) error {
	if len(args) != 3 {
		return fmt.Errorf("%s: invalid args", cmd.Use)
	}
	topopb, err := topo.Load(args[0])
	if err != nil {
		return fmt.Errorf("%s: %w", cmd.Use, err)
	}
	tOpts := append(opts, topo.WithKubecfg(viper.GetString("kubecfg")))
	tm, err := topo.New(topopb, tOpts...)
	if err != nil {
		return fmt.Errorf("%s: %w", cmd.Use, err)
	}

	fp, err := os.Open(args[2])
	if err != nil {
		return err
	}
	defer func() {
		if err := fp.Close(); err != nil {
			log.Warningf("failed to close config file %q", args[2])
		}
	}()
	return tm.ConfigPush(cmd.Context(), args[1], fp)
}

func watchFn(cmd *cobra.Command, args []string) error {
	if len(args) != 1 {
		return fmt.Errorf("%s: missing topology", cmd.Use)
	}
	topopb, err := topo.Load(args[0])
	if err != nil {
		return fmt.Errorf("%s: %w", cmd.Use, err)
	}
	tOpts := append(opts, topo.WithKubecfg(viper.GetString("kubecfg")))
	tm, err := topo.New(topopb, tOpts...)
	if err != nil {
		return fmt.Errorf("%s: %w", cmd.Use, err)
	}
	return tm.Watch(cmd.Context())
}

func certFn(cmd *cobra.Command, args []string) error {
	if len(args) != 2 {
		return fmt.Errorf("%s: missing args", cmd.Use)
	}
	topopb, err := topo.Load(args[0])
	if err != nil {
		return fmt.Errorf("%s: %w", cmd.Use, err)
	}
	tOpts := append(opts, topo.WithKubecfg(viper.GetString("kubecfg")))
	tm, err := topo.New(topopb, tOpts...)
	if err != nil {
		return fmt.Errorf("%s: %w", cmd.Use, err)
	}
	return tm.GenerateSelfSigned(cmd.Context(), args[1])
}

var newTopologyManager = func(topopb *tpb.Topology, opts ...topo.Option) (TopologyManager, error) {
	return topo.New(topopb, opts...)
}

type TopologyManager interface {
	Show(ctx context.Context) (*cpb.ShowTopologyResponse, error)
}

func serviceFn(cmd *cobra.Command, args []string) error {
	if len(args) != 1 {
		return fmt.Errorf("%s: missing topology", cmd.Use)
	}
	topopb, err := topo.Load(args[0])
	if err != nil {
		return fmt.Errorf("%s: %w", cmd.Use, err)
	}
	tOpts := append(opts, topo.WithKubecfg(viper.GetString("kubecfg")))
	tm, err := newTopologyManager(topopb, tOpts...)
	if err != nil {
		return fmt.Errorf("%s: %w", cmd.Use, err)
	}
	ts, err := tm.Show(cmd.Context())
	if err != nil {
		return err
	}

	fmt.Fprintln(cmd.OutOrStdout(), prototext.Format(ts.Topology))
	return nil
}
