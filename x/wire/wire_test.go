package wire

import (
	"context"
	"io"
	"testing"

	wpb "github.com/openconfig/kne/proto/wire"
	"google.golang.org/grpc/metadata"
)

type FakeStream struct {
	incoming []*wpb.Packet
	i        int
	outgoing []*wpb.Packet
}

func NewFakeStream(packets []*wpb.Packet) *FakeStream {
	return &FakeStream{incoming: packets, outgoing: []*wpb.Packet{}}
}

func (f *FakeStream) Recv() (*wpb.Packet, error) {
	if f.i >= len(f.incoming) {
		return nil, io.EOF
	}
	p := f.incoming[f.i]
	f.i++
	return p, nil
}

func (f *FakeStream) Send(p *wpb.Packet) error {
	f.outgoing = append(f.outgoing, p)
	return nil
}

type FakeReadWriter struct {
	src [][]byte
	i   int
	dst [][]byte
}

func NewFakeReadWriter(b [][]byte) *FakeReadWriter {
	return &FakeReadWriter{src: b, dst: [][]byte{}}
}

func (f *FakeReadWriter) Read() ([]byte, error) {
	if f.i >= len(f.src) {
		return nil, io.EOF
	}
	b := f.src[f.i]
	f.i++
	return b, nil
}

func (f *FakeReadWriter) Write(b []byte) error {
	f.dst = append(f.dst, b)
	return nil
}

func TestTransmit(t *testing.T) {
	rwSrc := [][]byte{
		[]byte("rwSrc1"),
		[]byte("rwSrc2"),
	}
	rw := NewFakeReadWriter(rwSrc)
	sSrc := []*wpb.Packet{
		{Data: []byte("sSrc1")},
		{Data: []byte("sSrc2")},
		{Data: []byte("sSrc3")},
	}
	s := NewFakeStream(sSrc)
	w := NewWire(rw)
	if err := w.Transmit(context.Background(), s); err != nil {
		t.Fatalf("wire.Transmit() unexpected error: %v", err)
	}
	if len(rw.dst) != len(s.incoming) {
		t.Fatalf("Got %v []bytes written, want %v", len(rw.dst), len(s.incoming))
	}
	for i := range rw.dst {
		if string(rw.dst[i]) != string(s.incoming[i].Data) {
			t.Errorf("Got data at index %v: %v, want %v", i, string(rw.dst[i]), string(s.incoming[i].Data))
		}
	}
	if len(s.outgoing) != len(rw.src) {
		t.Fatalf("Got %v packets written, want %v", len(s.outgoing), len(rw.src))
	}
	for i := range s.outgoing {
		if string(s.outgoing[i].Data) != string(rw.src[i]) {
			t.Errorf("Got data at index %v: %v, want %v", i, string(s.outgoing[i].Data), string(rw.src[i]))
		}
	}
}

func TestPhysicalEndpoint(t *testing.T) {
	pe := NewPhysicalEndpoint("dev1", "intf1")
	ctx := pe.NewContext(context.Background())
	md, ok := metadata.FromOutgoingContext(ctx)
	if !ok {
		t.Fatalf("Unable to get metadata from context")
	}
	ctx = metadata.NewIncomingContext(context.Background(), md)
	ppe, err := ParsePhysicalEndpoint(ctx)
	if err != nil {
		t.Fatalf("ParsePhysicalEndpoint() unexpected error: %v", err)
	}
	if ppe.device != pe.device {
		t.Errorf("ParsePhysicalEndpoint() got device %v, want device %v", ppe.device, pe.device)
	}
	if ppe.intf != pe.intf {
		t.Errorf("ParsePhysicalEndpoint() got intf %v, want intf %v", ppe.intf, pe.intf)
	}
}
