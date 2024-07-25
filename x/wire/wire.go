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

// Package wire provides a generic wire transport.
package wire

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"os"

	"github.com/google/gopacket"
	"github.com/google/gopacket/afpacket"
	"github.com/google/gopacket/layers"
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

type InterfaceReadWriter struct {
	handle *afpacket.TPacket
	ps     *gopacket.PacketSource
}

func NewInterfaceReadWriter(intf string) (*InterfaceReadWriter, error) {
	h, err := afpacket.NewTPacket(afpacket.OptInterface(intf))
	if err != nil {
		return nil, fmt.Errorf("unable to create packet handle for %q: %v", intf, err)
	}
	return &InterfaceReadWriter{handle: h, ps: gopacket.NewPacketSource(h, layers.LinkTypeEthernet)}, nil
}

func (i *InterfaceReadWriter) Read() ([]byte, error) {
	p, err := i.ps.NextPacket()
	if err != nil {
		return nil, err
	}
	return p.Data(), nil
}

func (i *InterfaceReadWriter) Write(b []byte) error {
	return i.handle.WritePacketData(b)
}

func (i *InterfaceReadWriter) Close() {
	i.handle.Close()
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

type InterfaceEndpoint struct {
	intf string
}

func NewInterfaceEndpoint(intf string) *InterfaceEndpoint {
	return &InterfaceEndpoint{intf: intf}
}

func ParseInterfaceEndpoint(ctx context.Context) (*InterfaceEndpoint, error) {
	md, ok := metadata.FromIncomingContext(ctx)
	if !ok {
		return nil, fmt.Errorf("no metadata in incoming context")
	}
	i := &InterfaceEndpoint{}
	vals := md.Get("interface")
	if len(vals) != 1 || vals[0] == "" {
		return nil, fmt.Errorf("interface key not found")
	}
	i.intf = vals[0]
	return i, nil
}

func (i *InterfaceEndpoint) NewContext(ctx context.Context) context.Context {
	md := metadata.New(map[string]string{
		"interface": i.intf,
	})
	return metadata.NewOutgoingContext(ctx, md)
}
