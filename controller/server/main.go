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
package main

import (
	"flag"
	"fmt"
	"log"
	"net"

	cpb "github.com/google/kne/proto/controller"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/alts"
)

var (
	port = flag.Int("port", 50051, "Controller server port")
)

type server struct {
	cpb.UnimplementedTopologyManagerServer
}

func main() {
	flag.Parse()
	addr := fmt.Sprintf(":%d", *port)
	lis, err := net.Listen("tcp6", addr)
	if err != nil {
		log.Fatalf("failed to listen: %v", err)
	}
	creds := alts.NewServerCreds(alts.DefaultServerOptions())
	s := grpc.NewServer(grpc.Creds(creds))
	cpb.RegisterTopologyManagerServer(s, &server{})
	log.Printf("Controller server listening at %v", lis.Addr())
	if err := s.Serve(lis); err != nil {
		log.Fatalf("failed to serve: %v", err)
	}
}
