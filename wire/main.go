package main

import (
	"flag"
	"fmt"
	"net"
	"io"

	log "github.com/golang/glog"
	wpb "github.com/openconfig/kne/proto/wire"
	"google.golang.org/grpc"
	"google.golang.org/grpc/metadata"
)

var (
	port = flag.Int("port", 50058, "Wire server port")
)

type server struct {
	wpb.UnimplementedWireServer
}

func newServer() *server {
	return &server{}
}

func (s *server) Transmit(stream wpb.Wire_TransmitServer) error {
	log.Infof("New Transmit stream started")
	md, ok := metadata.FromIncomingContext(stream.Context())
	if !ok {
		log.Infof("Missing metadata")
		return fmt.Errorf("missing metadata")
	}
	log.Infof("Got metadata: %v", md)
	for {
		in, err := stream.Recv()
		if err == io.EOF {
			return nil
		}
		if err != nil {
			return err
		}
		log.Infof("Recv packet: %v", string(in.Data))
		if err := stream.Send(in); err != nil {
			return err
		}
		log.Infof("Sent packet: %v", string(in.Data))
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
	wpb.RegisterWireServer(s, newServer())
	log.Infof("Wire server listening at %v", lis.Addr())
	if err := s.Serve(lis); err != nil {
		log.Fatalf("failed to serve: %v", err)
	}
}
