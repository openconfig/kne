// Copyright 2024 Google LLC
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

// Package main provides an example wire client.
package main

import (
	"context"
	"flag"

	wpb "github.com/openconfig/kne/proto/wire"
	"github.com/openconfig/kne/x/wire"
	"golang.org/x/sync/errgroup"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	log "k8s.io/klog/v2"
)

var (
	addr = flag.String("addr", "localhost:50058", "Wire server address")
)

func main() {
	ctx := context.Background()
	i0, err := wire.NewInterfaceReadWriter("eth0")
	if err != nil {
		log.Fatalf("Failed to create interface read/writer: %v", err)
	}
	i1, err := wire.NewInterfaceReadWriter("eth1")
	if err != nil {
		log.Fatalf("Failed to create interface read/writer: %v", err)
	}
	endpoints := map[*wire.PhysicalEndpoint]*wire.Wire{
		wire.NewPhysicalEndpoint("r3", "eth0"): wire.NewWire(i0),
		wire.NewPhysicalEndpoint("r3", "eth1"): wire.NewWire(i1),
	}
	opts := []grpc.DialOption{
		grpc.WithTransportCredentials(insecure.NewCredentials()),
	}
	conn, err := grpc.DialContext(ctx, *addr, opts...)
	if err != nil {
		log.Fatalf("Failed to dial %q: %v", *addr, err)
	}
	c := wpb.NewWireClient(conn)
	g := new(errgroup.Group)
	for e, w := range endpoints {
		e := e
		w := w
		g.Go(func() error {
			octx := e.NewContext(ctx)
			stream, err := c.Transmit(octx)
			if err != nil {
				return err
			}
			defer func() {
				stream.CloseSend()
			}()
			log.Infof("Transmitting endpoint %v over wire...", e)
			return w.Transmit(ctx, stream)
		})
	}
	if err := g.Wait(); err != nil {
		log.Fatalf("Failed to wait for wire transmits: %v", err)
	}
}
