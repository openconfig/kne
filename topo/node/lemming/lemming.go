// Copyright 2022 Google LLC
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

// Package lemming contains a node implementation for a lemming device.
package lemming

import (
	"context"
	"fmt"
	"io"

	"github.com/openconfig/kne/topo/node"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	tpb "github.com/openconfig/kne/proto/topo"
	log "github.com/sirupsen/logrus"
)

func New(nodeImpl *node.Impl) (node.Node, error) {
	if nodeImpl == nil {
		return nil, fmt.Errorf("nodeImpl cannot be nil")
	}
	if nodeImpl.Proto == nil {
		return nil, fmt.Errorf("nodeImpl.Proto cannot be nil")
	}
	cfg := defaults(nodeImpl.Proto)
	nodeImpl.Proto = cfg
	n := &Node{
		Impl: nodeImpl,
	}
	return n, nil
}

type Node struct {
	*node.Impl
}

// Add validations for interfaces the node provides
var (
	_ node.Certer       = (*Node)(nil)
	_ node.ConfigPusher = (*Node)(nil)
	_ node.Resetter     = (*Node)(nil)
)

func (n *Node) ResetCfg(ctx context.Context) error {
	log.Info("ResetCfg is a noop.")
	return nil
}

func (n *Node) ConfigPush(context.Context, io.Reader) error {
	return status.Errorf(codes.Unimplemented, "config push is not implemented using gNMI to configure device")
}

func (n *Node) GenerateSelfSigned(context.Context) error {
	return status.Errorf(codes.Unimplemented, "certificate generation is not supported")
}

func defaults(pb *tpb.Node) *tpb.Node {
	if pb.Config == nil {
		pb.Config = &tpb.Config{}
	}
	if pb.Config.Image == "" {
		pb.Config.Image = "lemming:latest"
	}
	if len(pb.GetConfig().GetCommand()) == 0 {
		pb.Config.Command = []string{"/lemming/lemming"}
	}
	if len(pb.GetConfig().GetArgs()) == 0 {
		pb.Config.Args = []string{"--alsologtostderr"}
	}
	if pb.Config.EntryCommand == "" {
		pb.Config.EntryCommand = fmt.Sprintf("kubectl exec -it %s -- /bin/bash", pb.Name)
	}
	if pb.Constraints == nil {
		pb.Constraints = map[string]string{
			"cpu":    "0.5",
			"memory": "1Gi",
		}
	}
	if pb.Labels == nil {
		pb.Labels = map[string]string{
			"type":   tpb.Node_LEMMING.String(),
			"vendor": tpb.Vendor_OPENCONFIG.String(),
		}
	}
	if pb.Services == nil {
		pb.Services = map[uint32]*tpb.Service{
			6030: {
				Name:    "gnmi",
				Inside:  6030,
				Outside: 6030,
			},
			6031: {
				Name:    "gribi",
				Inside:  6030,
				Outside: 6031,
			},
		}
	}
	return pb
}

func init() {
	node.Vendor(tpb.Vendor_OPENCONFIG, New)
}
