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
	"sync"
	"time"

	wpb "github.com/openconfig/kne/proto/wire"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/alts"
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
	// LinkOpener allows overriding the link-state constructor for testing.
	// Defaults to NewNetlinkLink. If it fails, the client bridges frames
	// without link state.
	LinkOpener func(ifaceName string) (Link, error)
	// RetryInterval is the delay before reconnecting if disconnected. Default 2s.
	RetryInterval time.Duration
	// UseALTS specifies whether to use ALTS transport credentials for DirectPath on GCE.
	UseALTS bool
}

// Client connects to a remote bridge server and bridges packets and link state to a local interface.
type Client struct {
	cfg          ClientConfig
	socketOpener func(ifaceName string) (ReadWriter, error)
	linkOpener   func(ifaceName string) (Link, error)
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

	linkOpener := cfg.LinkOpener
	if linkOpener == nil {
		linkOpener = NewNetlinkLink
	}

	return &Client{
		cfg:          cfg,
		socketOpener: opener,
		linkOpener:   linkOpener,
	}, nil
}

// Run establishes and maintains the bridge stream until the context is canceled.
func (c *Client) Run(ctx context.Context) error {
	dialOpts := c.cfg.DialOpts
	if len(dialOpts) == 0 {
		if c.cfg.UseALTS {
			dialOpts = []grpc.DialOption{
				grpc.WithTransportCredentials(alts.NewClientCreds(alts.DefaultClientOptions())),
			}
		} else {
			dialOpts = []grpc.DialOption{
				grpc.WithTransportCredentials(insecure.NewCredentials()),
			}
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

	client := wpb.NewWireClient(conn)
	streamCtx, streamCancel := context.WithCancel(ctx)

	link, err := c.linkOpener(c.cfg.LocalInterface)
	if err != nil {
		klog.Warningf("Link state of %s will not be propagated: %v", c.cfg.LocalInterface, err)
		link = nil
	}

	var wg sync.WaitGroup
	defer func() {
		streamCancel()
		_ = handler.Close()
		if link != nil {
			_ = link.Close()
		}
		wg.Wait()
	}()

	outCtx := metadata.NewOutgoingContext(streamCtx, metadata.Pairs("interface", c.cfg.RemoteInterface))
	stream, err := client.Transmit(outCtx)
	if err != nil {
		return fmt.Errorf("Wire.Transmit RPC failed: %w", err)
	}

	klog.Infof("Bridge client connected: local interface %s <-> remote %s (peer %s)",
		c.cfg.LocalInterface, c.cfg.RemoteInterface, c.cfg.PeerAddress)

	// sendMu serializes sends on the stream, which gRPC does not allow concurrently.
	var sendMu sync.Mutex
	send := func(p *wpb.Packet) error {
		sendMu.Lock()
		defer sendMu.Unlock()
		return stream.Send(p)
	}

	errChan := make(chan error, 3)

	// Link-state loop: report the carrier of the local interface to the server, starting with its current state.
	if link != nil {
		var reporter linkReporter
		if obs, err := link.Current(); err != nil {
			klog.Warningf("Failed to read link state of %s: %v", c.cfg.LocalInterface, err)
		} else if s := reporter.observe(obs); s != wpb.LinkState_LINK_STATE_UNKNOWN {
			if err := send(&wpb.Packet{LinkState: s}); err != nil {
				return fmt.Errorf("stream send error: %w", err)
			}
		}
		wg.Add(1)
		go func() {
			defer wg.Done()
			for obs := range link.Events() {
				s := reporter.observe(obs)
				if s == wpb.LinkState_LINK_STATE_UNKNOWN {
					continue
				}
				klog.Infof("[%s] Link state %v (admin up=%t, carrier=%t)", c.cfg.LocalInterface, s, obs.AdminUp, obs.Carrier)
				if err := send(&wpb.Packet{LinkState: s}); err != nil {
					if streamCtx.Err() != nil {
						return
					}
					select {
					case errChan <- fmt.Errorf("stream send error: %w", err):
					default:
					}
					return
				}
			}
		}()
	}

	wg.Add(2)

	// Egress loop: read from local raw socket, send to remote bridge server over gRPC
	go func() {
		defer wg.Done()
		for {
			pkt, err := handler.ReadPacket()
			if err != nil {
				if streamCtx.Err() != nil {
					return
				}
				select {
				case errChan <- fmt.Errorf("read from %s error: %w", c.cfg.LocalInterface, err):
				default:
				}
				return
			}
			if streamCtx.Err() != nil {
				return
			}
			if err := send(&wpb.Packet{Data: pkt}); err != nil {
				if streamCtx.Err() != nil {
					return
				}
				select {
				case errChan <- fmt.Errorf("stream send error: %w", err):
				default:
				}
				return
			}
		}
	}()

	// Ingress loop: receive from remote bridge server over gRPC, inject into local raw socket, and apply received
	// link state to the local interface.
	go func() {
		defer wg.Done()
		for {
			resp, err := stream.Recv()
			if err != nil {
				if streamCtx.Err() != nil {
					return
				}
				select {
				case errChan <- err:
				default:
				}
				return
			}
			if ls := resp.GetLinkState(); ls != wpb.LinkState_LINK_STATE_UNKNOWN {
				c.applyLinkState(link, ls)
			}
			pktData := resp.GetData()
			if len(pktData) > 0 {
				if len(pktData) > maxFrameSize {
					klog.Warningf("[%s] Dropped ingress packet: size %d exceeds max frame size %d", c.cfg.LocalInterface, len(pktData), maxFrameSize)
					continue
				}
				if err := handler.WritePacket(pktData); err != nil {
					if isFatalWriteError(err) {
						select {
						case errChan <- fmt.Errorf("write to %s error: %w", c.cfg.LocalInterface, err):
						default:
						}
						return
					}
					klog.Warningf("[%s] Dropped ingress packet: %v", c.cfg.LocalInterface, err)
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

// applyLinkState applies link state received from the server by setting the local interface administratively up
// or down.
func (c *Client) applyLinkState(link Link, s wpb.LinkState) {
	if link == nil {
		klog.V(1).Infof("[%s] Ignoring link state %v: link state is not available", c.cfg.LocalInterface, s)
		return
	}
	up := s == wpb.LinkState_LINK_STATE_UP
	klog.Infof("[%s] Setting admin up=%t for received link state %v", c.cfg.LocalInterface, up, s)
	if err := link.SetAdminUp(up); err != nil {
		klog.Errorf("[%s] Failed to apply link state %v: %v", c.cfg.LocalInterface, s, err)
	}
}
