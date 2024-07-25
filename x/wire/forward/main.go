package main

import (
	"context"
	"fmt"
	"net"
	"strings"

	wpb "github.com/openconfig/kne/proto/wire"
	"github.com/openconfig/kne/x/wire"
	flag "github.com/spf13/pflag"
	"golang.org/x/sync/errgroup"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	log "k8s.io/klog/v2"
)

var (
	port       = flag.Int("port", 50058, "Wire server port")
	interfaces = flag.StringSlice("interfaces", []string{}, "List of local interfaces to serve on the wire server")
	endpoints  = flag.StringSlice("endpoints", []string{}, "List of strings of the form local_interface/remote_address/remote_interface")
)

type server struct {
	wpb.UnimplementedWireServer
	endpoints map[wire.InterfaceEndpoint]*wire.Wire
}

func newServer(endpoints map[wire.InterfaceEndpoint]*wire.Wire) *server {
	return &server{endpoints: endpoints}
}

func (s *server) Transmit(stream wpb.Wire_TransmitServer) error {
	e, err := wire.ParseInterfaceEndpoint(stream.Context())
	if err != nil {
		return fmt.Errorf("unable to parse endpoint from incoming stream context: %v", err)
	}
	log.Infof("New Transmit stream started for endpoint %v", e)
	w, ok := s.endpoints[*e]
	if !ok {
		return fmt.Errorf("no endpoint found on server for request: %v", e)
	}
	if err := w.Transmit(stream.Context(), stream); err != nil {
		return fmt.Errorf("transmit failed: %v", err)
	}
	return nil
}

func localEndpoints(intfs []string) (map[wire.InterfaceEndpoint]*wire.Wire, error) {
	m := map[wire.InterfaceEndpoint]*wire.Wire{}
	for _, intf := range intfs {
		rw, err := wire.NewInterfaceReadWriter(intf)
		if err != nil {
			return nil, err
		}
		m[*wire.NewInterfaceEndpoint(intf)] = wire.NewWire(rw)
	}
	return m, nil
}

func remoteEndpoints(eps []string) (map[string]map[wire.InterfaceEndpoint]*wire.Wire, error) {
	m := map[string]map[wire.InterfaceEndpoint]*wire.Wire{}
	for _, ep := range eps {
		parts := strings.SplitN(ep, "/", 3)
		if len(parts) != 3 {
			return nil, fmt.Errorf("unable to parse %v into endpoint, got %v", ep, parts)
		}
		lintf := parts[0]
		addr := parts[1]
		rintf := parts[2]
		rw, err := wire.NewInterfaceReadWriter(lintf)
		if err != nil {
			return nil, err
		}
		if _, ok := m[addr]; !ok {
			m[addr] = map[wire.InterfaceEndpoint]*wire.Wire{}
		}
		m[addr][*wire.NewInterfaceEndpoint(rintf)] = wire.NewWire(rw)
	}
	return m, nil
}

func main() {
	flag.Parse()
	ctx := context.Background()
	addr := fmt.Sprintf(":%d", *port)
	lis, err := net.Listen("tcp6", addr)
	if err != nil {
		log.Fatalf("Failed to listen: %v", err)
	}
	s := grpc.NewServer()
	le, err := localEndpoints(*interfaces)
	if err != nil {
		log.Fatalf("Failed to create local endpoints from interfaces: %v")
	}
	wpb.RegisterWireServer(s, newServer(le))
	g := new(errgroup.Group)
	g.Go(func() error {
		log.Infof("Wire server listening at %v", lis.Addr())
		return s.Serve(lis)
	})
	re, err := remoteEndpoints(*endpoints)
	if err != nil {
		log.Fatalf("Failed to create remote endpoints: %v", err)
	}
	for a, m := range re {
		opts := []grpc.DialOption{
			grpc.WithTransportCredentials(insecure.NewCredentials()),
		}
		conn, err := grpc.DialContext(ctx, a, opts...)
		if err != nil {
			log.Fatalf("Failed to dial %q: %v", a, err)
		}
		defer conn.Close()
		c := wpb.NewWireClient(conn)
		for e, w := range m {
			c := c
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
	}
	if err := g.Wait(); err != nil {
		log.Fatalf("Failed to wait for wire transmits: %v", err)
	}
}
