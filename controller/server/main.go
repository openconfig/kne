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
	"context"
	"flag"
	"fmt"
	"log"
	"net"

	cpb "github.com/google/kne/proto/controller"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

var (
	port = flag.Int("port", 50051, "Controller server port")
)

type server struct {
	cpb.UnimplementedTopologyManagerServer
}

func (s *server) CreateCluster(ctx context.Context, req *cpb.CreateClusterRequest) (*cpb.CreateClusterResponse, error) {
	return nil, status.Errorf(codes.Unimplemented, "CreateCluster RPC method not implemented.")
}

func (s *server) DeleteCluster(ctx context.Context, req *cpb.DeleteClusterRequest) (*cpb.DeleteClusterResponse, error) {
	return nil, status.Errorf(codes.Unimplemented, "DeleteCluster RPC method not implemented.")
}

func (s *server) CreateTopology(ctx context.Context, req *cpb.CreateTopologyRequest) (*cpb.CreateTopologyResponse, error) {
	return nil, status.Errorf(codes.Unimplemented, "CreateTopology RPC method not implemented.")
}

func (s *server) DeleteTopology(ctx context.Context, req *cpb.DeleteTopologyRequest) (*cpb.DeleteTopologyResponse, error) {
	return nil, status.Errorf(codes.Unimplemented, "DeleteTopology RPC method not implemented.")
}

func main() {
	flag.Parse()
	lis, err := net.Listen("tcp", fmt.Sprintf("localhost:%d", *port))
	if err != nil {
		log.Fatalf("failed to listen: %v", err)
	}
	s := grpc.NewServer()
	cpb.RegisterTopologyManagerServer(s, &server{})

	log.Printf("Controller server listening at %v", lis.Addr())
	if err := s.Serve(lis); err != nil {
		log.Fatalf("failed to serve: %v", err)
	}
}
