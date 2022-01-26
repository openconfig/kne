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

	"github.com/google/kne/topo"
	"github.com/google/kne/topo/node"
	"github.com/openconfig/gnmi/errlist"
	log "github.com/sirupsen/logrus"
	"github.com/spf13/cobra"
	"google.golang.org/protobuf/encoding/prototext"
	corev1 "k8s.io/api/core/v1"

	tpb "github.com/google/kne/proto/topo"
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

func serviceToProto(s *corev1.Service, m map[uint32]*tpb.Service) error {
	if s == nil || m == nil {
		return fmt.Errorf("service and map must not be nil")
	}
	if len(s.Status.LoadBalancer.Ingress) == 0 {
		return fmt.Errorf("service %s has no external loadbalancer configured", s.Name)
	}
	for _, p := range s.Spec.Ports {
		k := uint32(p.Port)
		service, ok := m[k]
		if !ok {
			service = &tpb.Service{
				Name:   p.Name,
				Inside: k,
			}
			m[k] = service
		}
		if service.Name == "" {
			service.Name = p.Name
		}
		service.Outside = uint32(p.TargetPort.IntVal)
		service.NodePort = uint32(p.NodePort)
		service.InsideIp = s.Spec.ClusterIP
		service.OutsideIp = s.Status.LoadBalancer.Ingress[0].IP
	}
	return nil
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

func serviceFn(cmd *cobra.Command, args []string) error {
	if len(args) != 1 {
		return fmt.Errorf("%s: missing topology", cmd.Use)
	}
	topopb, err := topo.Load(args[0])
	if err != nil {
		return fmt.Errorf("%s: %w", cmd.Use, err)
	}
	kubeCfg, err := cmd.Flags().GetString("kubecfg")
	if err != nil {
		return err
	}
	t, err := topoNew(kubeCfg, topopb, opts...)
	if err != nil {
		return fmt.Errorf("%s: %w", cmd.Use, err)
	}
	if err := t.Load(cmd.Context()); err != nil {
		return fmt.Errorf("%s: %w", cmd.Use, err)
	}

	r, err := t.Resources(cmd.Context())
	if err != nil {
		return err
	}
	for _, n := range topopb.Nodes {
		if len(n.Services) == 0 {
			n.Services = map[uint32]*tpb.Service{}
		}
		// (TODO:hines): Remove type once deprecated
		if n.Vendor == tpb.Vendor_KEYSIGHT || n.Type == tpb.Node_IXIA_TG {
			// Add Keysight gnmi and grpc global services until
			// they have a better registration mechanism for global
			// services
			if gnmiService, ok := r.Services["gnmi-service"]; ok {
				serviceToProto(gnmiService, n.Services)
			}
			if grpcService, ok := r.Services["grpc-service"]; ok {
				serviceToProto(grpcService, n.Services)
			}
		}
		sName := fmt.Sprintf("service-%s", n.Name)
		s, ok := r.Services[sName]
		if !ok {
			return fmt.Errorf("service %s not found", sName)
		}
		serviceToProto(s, n.Services)
	}
	fmt.Fprintln(cmd.OutOrStdout(), prototext.Format(topopb))
	return nil
}
