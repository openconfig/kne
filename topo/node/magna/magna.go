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

// Package magna implements the node definition for a magna container in KNE.
package magna

import (
	"fmt"

	tpb "github.com/openconfig/kne/proto/topo"
	"github.com/openconfig/kne/topo/node"
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

func defaults(pb *tpb.Node) *tpb.Node {
	if pb.Config == nil {
		pb.Config = &tpb.Config{}
	}
	if pb.Services == nil {
		pb.Services = map[uint32]*tpb.Service{}
	}
	if len(pb.GetConfig().GetCommand()) == 0 {
		pb.Config.Command = []string{
			"/app/magna",
			"-v=2",
			"-alsologtostderr",
			"-port=40051",
			"-telemetry_port=50051",
			"-certfile=/data/cert.pem",
			"-keyfile=/data/key.pem",
		}
	}
	if pb.Config.EntryCommand == "" {
		pb.Config.EntryCommand = fmt.Sprintf("kubectl exec -it %s -- sh", pb.Name)
	}
	if pb.Config.Image == "" {
		// TODO(robjs): add public container location once the first iteration is pushed.
		// Currently, this image can be built from github.com/openconfig/magna.
		pb.Config.Image = "magna:latest"
	}

	if _, ok := pb.Services[40051]; !ok {
		pb.Services[40051] = &tpb.Service{
			Name:    "grpc",
			Inside:  40051,
			Outside: 40051,
		}
	}

	if _, ok := pb.Services[50051]; !ok {
		pb.Services[50051] = &tpb.Service{
			Name:    "gnmi",
			Inside:  50051,
			Outside: 50051,
		}
	}

	return pb
}

func init() {
	node.Vendor(tpb.Vendor_MAGNA, New)
}
