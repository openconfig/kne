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

// Package main provides an example wire server.
package main

import (
	"flag"
	"fmt"
	"net"

	log "github.com/golang/glog"
	wpb "github.com/openconfig/kne/proto/wire"
	"github.com/openconfig/kne/x/wire"
	"google.golang.org/grpc"
)

var (
	port = flag.Int("port", 50058, "Wire server port")
)

type server struct {
	wpb.UnimplementedWireServer
	endpoints map[wire.PhysicalEndpoint]*wire.Wire
}

func newServer(endpoints map[wire.PhysicalEndpoint]*wire.Wire) *server {
	return &server{endpoints: endpoints}
}

func (s *server) Transmit(stream wpb.Wire_TransmitServer) error {
	pe, err := wire.ParsePhysicalEndpoint(stream.Context())
	if err != nil {
		return fmt.Errorf("unable to parse physical endpoint from incoming stream context: %v", err)
	}
	log.Infof("New Transmit stream started for endpoint %v", pe)
	w, ok := s.endpoints[*pe]
	if !ok {
		return fmt.Errorf("no endpoint found on server for request: %v", pe)
	}
	if err := w.Transmit(stream.Context(), stream); err != nil {
		return fmt.Errorf("transmit failed: %v", err)
	}
	return nil
}

func main() {
	flag.Parse()
	addr := fmt.Sprintf(":%d", *port)
	lis, err := net.Listen("tcp6", addr)
	if err != nil {
		log.Fatalf("failed to listen: %v", err)
	}
	s := grpc.NewServer()
	i0, err := wire.NewInterfaceReadWriter("eth0")
	if err != nil {
		log.Fatalf("Failed to create packet handle read/writer: %v", err)
	}
	i1, err := wire.NewInterfaceReadWriter("eth1")
	if err != nil {
		log.Fatalf("Failed to create file based read/writer: %v", err)
	}
	endpoints := map[wire.PhysicalEndpoint]*wire.Wire{
		*wire.NewPhysicalEndpoint("r3", "eth0"): wire.NewWire(i0),
		*wire.NewPhysicalEndpoint("r3", "eth1"): wire.NewWire(i1),
	}
	wpb.RegisterWireServer(s, newServer(endpoints))
	log.Infof("Wire server listening at %v", lis.Addr())
	if err := s.Serve(lis); err != nil {
		log.Fatalf("failed to serve: %v", err)
	}
}
