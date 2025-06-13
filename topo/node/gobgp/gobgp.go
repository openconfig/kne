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
package gobgp

import (
	"fmt"

	tpb "github.com/openconfig/kne/proto/topo"
	"github.com/openconfig/kne/topo/node"
	"google.golang.org/protobuf/proto"
)

var (
	defaultNode = tpb.Node{
		Name: "default_gobgp_node",
		Config: &tpb.Config{
			Image:        "hfam/gobgp:latest",
			ConfigPath:   "/",
			ConfigFile:   "gobgp.conf",
			EntryCommand: fmt.Sprintf("kubectl exec -it %s -- /bin/bash", "default_gobgp_node"),
			Command:      []string{"/usr/local/bin/gobgpd", "-f", "/gobgp.conf", "-t", "yaml"},
		},
	}
)

func New(nodeImpl *node.Impl) (node.Node, error) {
	if nodeImpl == nil {
		return nil, fmt.Errorf("nodeImpl cannot be nil")
	}
	if nodeImpl.Proto == nil {
		return nil, fmt.Errorf("nodeImpl.Proto cannot be nil")
	}
	defaults(nodeImpl.Proto)
	n := &Node{
		Impl: nodeImpl,
	}
	return n, nil
}

type Node struct {
	*node.Impl
}

func defaults(pb *tpb.Node) *tpb.Node {
	defaultNodeClone := proto.Clone(&defaultNode).(*tpb.Node)
	if pb.Config == nil {
		pb.Config = &tpb.Config{}
	}
	if pb.Config.Image == "" {
		pb.Config.Image = defaultNodeClone.Config.Image
	}
	if len(pb.GetConfig().GetCommand()) == 0 {
		pb.Config.Command = defaultNode.Config.Command
	}
	if pb.Config.EntryCommand == "" {
		pb.Config.EntryCommand = fmt.Sprintf("kubectl exec -it %s -- /bin/bash", pb.Name)
	}
	if pb.Config.ConfigPath == "" {
		pb.Config.ConfigPath = defaultNode.Config.ConfigPath
	}
	if pb.Config.ConfigFile == "" {
		pb.Config.ConfigFile = defaultNodeClone.Config.ConfigFile
	}
	return pb
}

func init() {
	node.Vendor(tpb.Vendor_GOBGP, New)
}
