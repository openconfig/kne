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
	"bytes"
	"context"
	"net"
	"testing"
	"time"

	wpb "github.com/openconfig/kne/proto/wire"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/test/bufconn"
)

func TestBridgeClientBidirectionalForwarding(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	// 1. Setup Server
	server := NewServer(ctx)
	defer func() {
		_ = server.Close()
	}()

	serverIO := newFakeReadWriter()
	server.SetSocketOpener(func(ifaceName string) (ReadWriter, error) {
		return serverIO, nil
	})

	lis := bufconn.Listen(1024 * 1024)
	grpcServer := grpc.NewServer()
	wpb.RegisterWireServer(grpcServer, server)

	go func() {
		_ = grpcServer.Serve(lis)
	}()
	defer grpcServer.Stop()

	// 2. Setup Client
	clientIO := newFakeReadWriter()
	client, err := NewClient(ClientConfig{
		PeerAddress:     "passthrough://bufnet",
		LocalInterface:  "eth1",
		RemoteInterface: "eth1",
		RetryInterval:   100 * time.Millisecond,
		DialOpts: []grpc.DialOption{
			grpc.WithContextDialer(func(context.Context, string) (net.Conn, error) {
				return lis.Dial()
			}),
			grpc.WithTransportCredentials(insecure.NewCredentials()),
		},
		SocketOpener: func(ifaceName string) (ReadWriter, error) {
			return clientIO, nil
		},
	})
	if err != nil {
		t.Fatalf("NewClient failed: %v", err)
	}

	clientCtx, clientCancel := context.WithCancel(ctx)
	defer clientCancel()

	go func() {
		_ = client.Run(clientCtx)
	}()

	// 3. Test Client -> Server flow
	pktClientToServer := []byte{0xDE, 0xAD, 0xBE, 0xEF}
	clientIO.readChan <- pktClientToServer

	// Trigger server to open its demux/socket by reading from server side
	// Server receives pktClientToServer from gRPC and writes to serverIO
	select {
	case received := <-serverIO.writeChan:
		if !bytes.Equal(received, pktClientToServer) {
			t.Fatalf("Server write mismatch: got %v, want %v", received, pktClientToServer)
		}
	case <-time.After(2 * time.Second):
		t.Fatalf("Timed out waiting for Client -> Server packet")
	}

	// 4. Test Server -> Client flow
	pktServerToClient := []byte{0xCA, 0xFE, 0xBA, 0xBE}
	serverIO.readChan <- pktServerToClient

	select {
	case received := <-clientIO.writeChan:
		if !bytes.Equal(received, pktServerToClient) {
			t.Fatalf("Client write mismatch: got %v, want %v", received, pktServerToClient)
		}
	case <-time.After(2 * time.Second):
		t.Fatalf("Timed out waiting for Server -> Client packet")
	}
}

func TestBridgeClientShutdown(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())

	clientIO := newFakeReadWriter()
	client, err := NewClient(ClientConfig{
		PeerAddress:    "nonexistent:50058",
		LocalInterface: "eth1",
		RetryInterval:  50 * time.Millisecond,
		SocketOpener: func(ifaceName string) (ReadWriter, error) {
			return clientIO, nil
		},
	})
	if err != nil {
		t.Fatalf("NewClient failed: %v", err)
	}

	errCh := make(chan error, 1)
	go func() {
		errCh <- client.Run(ctx)
	}()

	// Cancel context after brief moment
	time.Sleep(50 * time.Millisecond)
	cancel()

	select {
	case err := <-errCh:
		if err != context.Canceled {
			t.Fatalf("Expected context.Canceled, got %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatalf("Timed out waiting for client to shut down")
	}
}

func TestNewClientValidation(t *testing.T) {
	// Case 1: Empty peer address
	if _, err := NewClient(ClientConfig{}); err == nil {
		t.Fatalf("Expected error for empty PeerAddress, got nil")
	}

	// Case 2: Defaults populated
	c, err := NewClient(ClientConfig{PeerAddress: "localhost:50058"})
	if err != nil {
		t.Fatalf("NewClient failed: %v", err)
	}
	if c.cfg.LocalInterface != "eth1" {
		t.Errorf("got LocalInterface = %q, want %q", c.cfg.LocalInterface, "eth1")
	}
	if c.cfg.RemoteInterface != "eth1" {
		t.Errorf("got RemoteInterface = %q, want %q", c.cfg.RemoteInterface, "eth1")
	}
	if c.cfg.RetryInterval != 2*time.Second {
		t.Errorf("got RetryInterval = %v, want %v", c.cfg.RetryInterval, 2*time.Second)
	}
	if c.socketOpener == nil {
		t.Errorf("expected socketOpener to be set")
	}

	// Case 3: Custom values preserved
	c2, err := NewClient(ClientConfig{
		PeerAddress:     "192.168.1.1:50058",
		LocalInterface:  "eth2",
		RemoteInterface: "eth3",
		RetryInterval:   5 * time.Second,
	})
	if err != nil {
		t.Fatalf("NewClient failed: %v", err)
	}
	if c2.cfg.LocalInterface != "eth2" {
		t.Errorf("got LocalInterface = %q, want %q", c2.cfg.LocalInterface, "eth2")
	}
	if c2.cfg.RemoteInterface != "eth3" {
		t.Errorf("got RemoteInterface = %q, want %q", c2.cfg.RemoteInterface, "eth3")
	}
	if c2.cfg.RetryInterval != 5*time.Second {
		t.Errorf("got RetryInterval = %v, want %v", c2.cfg.RetryInterval, 5*time.Second)
	}
}
