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
	"fmt"
	"os"

	"github.com/google/kne/topo"
	"github.com/google/kne/topo/node"
	"github.com/spf13/cobra"
	"google.golang.org/protobuf/encoding/prototext"

	tpb "github.com/google/kne/proto/topo"
)

func New() *cobra.Command {
	topoCmd := &cobra.Command{
		Use:   "topology",
		Short: "Topology commands.",
	}
	topoCmd.AddCommand(certCmd)
	topoCmd.AddCommand(pushCmd)
	topoCmd.AddCommand(serviceCmd)
	topoCmd.AddCommand(watchCmd)
	resetCfgCmd.Flags().BoolVar(&skipReset, "skip", skipReset, "skip nodes if they are not resetable")
	topoCmd.AddCommand(resetCfgCmd)
	return topoCmd
}

var (
	pushCmd = &cobra.Command{
		Use:   "push <topology> <device> <config file>",
		Short: "push config to device",
		RunE:  pushFn,
	}
	watchCmd = &cobra.Command{
		Use:   "watch <topology>",
		Short: "watch will watch the current topologies",
		RunE:  watchFn,
	}
	serviceCmd = &cobra.Command{
		Use:   "service <topology>",
		Short: "service returns the current topology with service endpoints defined.",
		RunE:  serviceFn,
	}
	certCmd = &cobra.Command{
		Use:   "cert <topology> <device>",
		Short: "push or generate certs for nodes in topology",
		RunE:  certFn,
	}
	resetCfgCmd = &cobra.Command{
		Use:   "reset -skip <topology> <device>",
		Short: "reset configuration of device (if device not provide reset all nodes)",
		RunE:  resetCfgFn,
	}
)

var (
	skipReset bool
)

func resetCfgFn(cmd *cobra.Command, args []string) error {
	if len(args) != 1 {
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
	ctx := cmd.Context()
	t.Load(ctx)
	var nodes []*node.Node
	if len(args) == 1 {
		nodes = t.Nodes()
	} else {
		n, err := t.Node(args[1])
		if err != nil {
			return err
		}
		nodes = append(nodes, n)
	}
	var resetable []node.Reseter
	for _, n := range nodes {
		r, ok := n.Impl().(node.Reseter)
		if !ok {
			if skipReset {
				continue
			}
			return fmt.Errorf("node %s is not resetable and --skip not set", n.Name())
		}
		resetable = append(resetable, r)
	}
	for _, r := range resetable {
		if err := r.ResetCfg(ctx); err != nil {
			return err
		}
	}
	return nil
}

func pushFn(cmd *cobra.Command, args []string) error {
	if len(args) != 3 {
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
	ctx := cmd.Context()
	t.Load(ctx)
	fp, err := os.Open(args[2])
	if err != nil {
		return err
	}
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

func serviceFn(cmd *cobra.Command, args []string) error {
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
	r, err := t.Resources(cmd.Context())
	if err != nil {
		return err
	}
	for _, n := range topopb.Nodes {
		sName := fmt.Sprintf("service-%s", n.Name)
		s, ok := r.Services[sName]
		if !ok {
			return fmt.Errorf("service %s not found", sName)
		}
		if len(s.Status.LoadBalancer.Ingress) == 0 {
			return fmt.Errorf("service %s has no external loadbalancer configured", sName)
		}
		if n.Services == nil {
			n.Services = map[uint32]*tpb.Service{}
		}
		for _, p := range s.Spec.Ports {
			k := uint32(p.Port)
			service, ok := n.Services[k]
			if !ok {
				service = &tpb.Service{
					Name:   p.Name,
					Inside: uint32(p.Port),
				}
				n.Services[k] = service
			}
			service.Outside = uint32(p.TargetPort.IntVal)
			service.NodePort = uint32(p.NodePort)
			service.InsideIp = s.Spec.ClusterIP
			service.OutsideIp = s.Status.LoadBalancer.Ingress[0].IP
		}
	}
	b, err := prototext.Marshal(topopb)
	if err != nil {
		return err
	}
	fmt.Fprintln(cmd.OutOrStdout(), string(b))
	return nil
}
