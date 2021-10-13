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
package gobgp

import (
	"fmt"

	tpb "github.com/google/kne/proto/topo"
	"github.com/google/kne/topo/node"
	"google.golang.org/protobuf/proto"
)

func New(nodeImpl *node.Impl) (node.Node, error) {
	cfg := defaults(nodeImpl.Proto)
	proto.Merge(cfg, nodeImpl.Proto)
	node.FixServices(cfg)
	n := &Node{
		Impl: nodeImpl,
	}
	proto.Merge(n.Impl.Proto, cfg)
	return n, nil
}

type Node struct {
	*node.Impl
}

func defaults(pb *tpb.Node) *tpb.Node {
	return &tpb.Node{
		Config: &tpb.Config{
			Image:        "hfam/gobgp:latest",
			Command:      []string{"/usr/local/bin/gobgpd", "-f", "/gobgp.conf", "-t", "yaml"},
			EntryCommand: fmt.Sprintf("kubectl exec -it %s -- /bin/bash", pb.Name),
			ConfigPath:   "/",
			ConfigFile:   "gobgp.conf",
		},
	}
}

func init() {
	node.Register(tpb.Node_GOBGP, New)
	node.Vendor(tpb.Vendor_GOBGP, New)
}
