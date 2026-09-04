// Copyright 2026 Google LLC
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

package bridge

import (
	"context"
	"fmt"
	"io"
	"time"

	wpb "github.com/openconfig/kne/proto/wire"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/metadata"
	"k8s.io/klog/v2"
)

// ClientConfig holds configuration for the bridge client.
type ClientConfig struct {
	// PeerAddress is the host:port of the remote bridge server.
	PeerAddress string
	// LocalInterface is the local network interface to bridge (e.g., "eth1").
	LocalInterface string
	// RemoteInterface is the remote interface name passed in gRPC metadata. Defaults to LocalInterface.
	RemoteInterface string
	// DialOpts allows custom gRPC dial options (useful for in-memory testing).
	DialOpts []grpc.DialOption
	// SocketOpener allows overriding the raw socket constructor for testing.
	SocketOpener func(ifaceName string) (ReadWriter, error)
	// RetryInterval is the delay before reconnecting if disconnected. Default 2s.
	RetryInterval time.Duration
}

// Client connects to a remote bridge server and bridges packets to a local interface.
type Client struct {
	cfg          ClientConfig
	socketOpener func(ifaceName string) (ReadWriter, error)
}

// NewClient constructs a new bridge Client.
func NewClient(cfg ClientConfig) (*Client, error) {
	if cfg.PeerAddress == "" {
		return nil, fmt.Errorf("peer address cannot be empty")
	}
	if cfg.LocalInterface == "" {
		cfg.LocalInterface = "eth1"
	}
	if cfg.RemoteInterface == "" {
		cfg.RemoteInterface = cfg.LocalInterface
	}
	if cfg.RetryInterval <= 0 {
		cfg.RetryInterval = 2 * time.Second
	}

	opener := cfg.SocketOpener
	if opener == nil {
		opener = func(ifaceName string) (ReadWriter, error) {
			return NewSocketHandler(ifaceName)
		}
	}

	return &Client{
		cfg:          cfg,
		socketOpener: opener,
	}, nil
}

// Run establishes and maintains the bridge stream until the context is canceled.
func (c *Client) Run(ctx context.Context) error {
	dialOpts := c.cfg.DialOpts
	if len(dialOpts) == 0 {
		dialOpts = []grpc.DialOption{
			grpc.WithTransportCredentials(insecure.NewCredentials()),
		}
	}

	for {
		if ctx.Err() != nil {
			return ctx.Err()
		}

		klog.Infof("Connecting to bridge server at %s...", c.cfg.PeerAddress)
		conn, err := grpc.NewClient(c.cfg.PeerAddress, dialOpts...)
		if err != nil {
			klog.Errorf("Failed to dial %s: %v. Retrying in %v...", c.cfg.PeerAddress, err, c.cfg.RetryInterval)
			select {
			case <-time.After(c.cfg.RetryInterval):
				continue
			case <-ctx.Done():
				return ctx.Err()
			}
		}

		err = c.runStream(ctx, conn)
		_ = conn.Close()

		if ctx.Err() != nil {
			return ctx.Err()
		}

		if err == nil || err == io.EOF {
			klog.Infof("Bridge stream closed by peer. Reconnecting in %v...", c.cfg.RetryInterval)
		} else {
			klog.Warningf("Bridge stream disconnected: %v. Reconnecting in %v...", err, c.cfg.RetryInterval)
		}
		select {
		case <-time.After(c.cfg.RetryInterval):
		case <-ctx.Done():
			return ctx.Err()
		}
	}
}

func (c *Client) runStream(ctx context.Context, conn *grpc.ClientConn) error {
	handler, err := c.socketOpener(c.cfg.LocalInterface)
	if err != nil {
		return fmt.Errorf("failed to open local interface %s: %w", c.cfg.LocalInterface, err)
	}
	defer func() {
		_ = handler.Close()
	}()

	client := wpb.NewWireClient(conn)
	streamCtx, streamCancel := context.WithCancel(ctx)
	defer streamCancel()

	outCtx := metadata.NewOutgoingContext(streamCtx, metadata.Pairs("interface", c.cfg.RemoteInterface))
	stream, err := client.Transmit(outCtx)
	if err != nil {
		return fmt.Errorf("Wire.Transmit RPC failed: %w", err)
	}

	klog.Infof("Bridge client connected: local interface %s <-> remote %s (peer %s)",
		c.cfg.LocalInterface, c.cfg.RemoteInterface, c.cfg.PeerAddress)

	errChan := make(chan error, 2)

	// Egress loop: read from local raw socket, send to remote bridge server over gRPC
	go func() {
		for {
			select {
			case <-streamCtx.Done():
				errChan <- streamCtx.Err()
				return
			default:
				pkt, err := handler.ReadPacket()
				if err != nil {
					errChan <- fmt.Errorf("read from %s error: %w", c.cfg.LocalInterface, err)
					return
				}
				if err := stream.Send(&wpb.Packet{Data: pkt}); err != nil {
					errChan <- fmt.Errorf("stream send error: %w", err)
					return
				}
			}
		}
	}()

	// Ingress loop: receive from remote bridge server over gRPC, inject into local raw socket
	go func() {
		for {
			resp, err := stream.Recv()
			if err != nil {
				errChan <- err
				return
			}
			pktData := resp.GetData()
			if len(pktData) > 0 {
				if err := handler.WritePacket(pktData); err != nil {
					errChan <- fmt.Errorf("write to %s error: %w", c.cfg.LocalInterface, err)
					return
				}
			}
		}
	}()

	select {
	case err := <-errChan:
		if err == io.EOF || streamCtx.Err() != nil {
			return nil
		}
		return err
	case <-ctx.Done():
		return ctx.Err()
	}
}
