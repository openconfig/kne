package main

import (
	"context"
	"flag"
	"io"
	"fmt"

	log "github.com/golang/glog"
	wpb "github.com/openconfig/kne/proto/wire"
	"google.golang.org/grpc"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/credentials/insecure"
)

var (
	addr = flag.String("addr", "localhost:50058", "Wire server address")
)

func main() {
	ctx := context.Background()
	opts := []grpc.DialOption{
		grpc.WithTransportCredentials(insecure.NewCredentials()),
	}
	conn, err := grpc.DialContext(ctx, *addr, opts...)
	if err != nil {
		log.Fatalf("Failed to dial %q: %v", *addr, err)
	}
	defer conn.Close()
	c := wpb.NewWireClient(conn)
	mctx := metadata.NewOutgoingContext(ctx, metadata.New(map[string]string{
		"device": "xx01.sql17",
		"intf": "HundredGigE0/0/0/0",
	}))
	stream, err := c.Transmit(mctx)
	waitc := make(chan struct{})
	if err != nil {
		log.Fatalf("Failed to create transmit stream: %v", err)
	}
	go func() {
		for {
    			in, err := stream.Recv()
    			if err == io.EOF {
      				// read done.
      				close(waitc)
      				return
    			}
    			if err != nil {
      				log.Fatalf("Failed to receive a packet: %v", err)
    			}
			log.Infof("Recv packet: %v", in.Data)
  		}
	}()
	for i := 0; i < 10; i++ {
		p := &wpb.Packet{
			Data:   []byte(fmt.Sprintf("packet %v", i)),
		}
		if err := stream.Send(p); err != nil {
			log.Fatalf("Failed to send packet %v: %v", i, err)
		}
		log.Infof("Sent packet: %v", p.Data)
	}
	stream.CloseSend()
	<-waitc
}
