package wire

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"os"

	wpb "github.com/openconfig/kne/proto/wire"
	"golang.org/x/sync/errgroup"
	"google.golang.org/grpc"
	"google.golang.org/grpc/metadata"
	log "k8s.io/klog/v2"
)

type ReadWriter interface {
	Read() ([]byte, error)
	Write([]byte) error
}

type FileReadWriter struct {
	src        *os.File
	srcScanner *bufio.Scanner
	dst        *os.File
}

func NewFileReadWriter(srcPath string, dstPath string) (*FileReadWriter, error) {
	src, err := os.Open(srcPath)
	if err != nil {
		return nil, err
	}
	dst, err := os.Create(dstPath)
	if err != nil {
		return nil, err
	}
	f := &FileReadWriter{
		src:        src,
		srcScanner: bufio.NewScanner(src),
		dst:        dst,
	}
	return f, nil
}

func (f *FileReadWriter) Read() ([]byte, error) {
	if ok := f.srcScanner.Scan(); ok {
		return append(f.srcScanner.Bytes(), []byte("\n")...), nil
	}
	if err := f.srcScanner.Err(); err != nil {
		return nil, err
	}
	return nil, io.EOF
}

func (f *FileReadWriter) Write(b []byte) error {
	if _, err := f.dst.Write(b); err != nil {
		return err
	}
	return nil
}

func (f *FileReadWriter) Close() error {
	if err := f.src.Close(); err != nil {
		return err
	}
	if err := f.dst.Close(); err != nil {
		return err
	}
	return nil
}

type Stream interface {
	Recv() (*wpb.Packet, error)
	Send(*wpb.Packet) error
}

type Wire struct {
	src ReadWriter
}

func NewWire(src ReadWriter) *Wire {
	return &Wire{src: src}
}

func (w *Wire) Transmit(ctx context.Context, stream Stream) error {
	g := new(errgroup.Group)
	g.Go(func() error {
		for {
			p, err := stream.Recv()
			if err == io.EOF {
				// read done.
				log.Infof("EOF recv from stream")
				return nil
			}
			if err != nil {
				return fmt.Errorf("failed to receive packet: %v", err)
			}
			log.Infof("Recv packet: %q", string(p.Data))
			if err := w.src.Write(p.Data); err != nil {
				return fmt.Errorf("failed to write packet: %v", err)
			}
			log.Infof("Wrote packet: %q", string(p.Data))
		}
	})
	g.Go(func() error {
		if cs, ok := stream.(grpc.ClientStream); ok {
			defer cs.CloseSend()
		}
		for {
			data, err := w.src.Read()
			if err == io.EOF {
				// read done.
				log.Infof("EOF reading from src")
				return nil
			}
			if err != nil {
				return fmt.Errorf("failed to read packet: %v", err)
			}
			p := &wpb.Packet{Data: data}
			log.Infof("Read packet: %q", string(p.Data))
			if err := stream.Send(p); err != nil {
				return fmt.Errorf("failed to send packet: %v", err)
			}
			log.Infof("Sent packet: %q", string(p.Data))
		}
	})
	return g.Wait()
}

type PhysicalEndpoint struct {
	device string
	intf   string
}

func NewPhysicalEndpoint(device, intf string) *PhysicalEndpoint {
	return &PhysicalEndpoint{device: device, intf: intf}
}

func ParsePhysicalEndpoint(ctx context.Context) (*PhysicalEndpoint, error) {
	md, ok := metadata.FromIncomingContext(ctx)
	if !ok {
		return nil, fmt.Errorf("no metadata in incoming context")
	}
	p := &PhysicalEndpoint{}
	vals := md.Get("device")
	if len(vals) != 1 || vals[0] == "" {
		return nil, fmt.Errorf("device key not found")
	}
	p.device = vals[0]
	vals = md.Get("interface")
	if len(vals) != 1 || vals[0] == "" {
		return nil, fmt.Errorf("interface key not found")
	}
	p.intf = vals[0]
	return p, nil
}

func (p *PhysicalEndpoint) NewContext(ctx context.Context) context.Context {
	md := metadata.New(map[string]string{
		"device":    p.device,
		"interface": p.intf,
	})
	return metadata.NewOutgoingContext(ctx, md)
}
