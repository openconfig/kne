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
	"net"
	"testing"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/test/bufconn"

	wpb "github.com/openconfig/kne/proto/wire"
)

// TestClientServerLinkStatePropagation bridges two veth-like links through a
// client and a server, as in the paired-bridge example:
//
//	host1 <-veth-> server link <-Wire-> client link <-veth-> host2
//
// When host1 goes admin down, the server link loses carrier, the server
// reports DOWN, and the client sets its link admin down, which host2 would see
// as loss of carrier; and the same in the other direction.
func TestClientServerLinkStatePropagation(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	serverLink := newFakeLink(true, true)
	clientLink := newFakeLink(true, true)

	server := NewServer(ctx)
	defer func() { _ = server.Close() }()
	server.SetSocketOpener(func(string) (ReadWriter, error) { return newFakeReadWriter(), nil })
	server.SetLinkOpener(func(string) (Link, error) { return serverLink, nil })

	lis := bufconn.Listen(1024 * 1024)
	grpcServer := grpc.NewServer()
	wpb.RegisterWireServer(grpcServer, server)
	go func() { _ = grpcServer.Serve(lis) }()
	defer grpcServer.Stop()

	client, err := NewClient(ClientConfig{
		PeerAddress:    "passthrough://bufnet",
		LocalInterface: "eth1",
		RetryInterval:  50 * time.Millisecond,
		DialOpts: []grpc.DialOption{
			grpc.WithContextDialer(func(context.Context, string) (net.Conn, error) { return lis.Dial() }),
			grpc.WithTransportCredentials(insecure.NewCredentials()),
		},
		SocketOpener: func(string) (ReadWriter, error) { return newFakeReadWriter(), nil },
		LinkOpener:   func(string) (Link, error) { return clientLink, nil },
	})
	if err != nil {
		t.Fatalf("NewClient failed: %v", err)
	}
	clientCtx, clientCancel := context.WithCancel(ctx)
	defer clientCancel()
	go func() { _ = client.Run(clientCtx) }()

	// Both ends report UP on connect; each side applies the other's UP.
	if up := serverLink.nextSet(t); !up {
		t.Fatalf("server link SetAdminUp(%t) on connect, want true", up)
	}
	if up := clientLink.nextSet(t); !up {
		t.Fatalf("client link SetAdminUp(%t) on connect, want true", up)
	}

	// host1 goes down: the client link is set admin down.
	serverLink.setPeerUp(false)
	if up := clientLink.nextSet(t); up {
		t.Errorf("client link SetAdminUp(%t) after host1 down, want false", up)
	}
	// host1 comes back: the client link is set admin up again.
	serverLink.setPeerUp(true)
	if up := clientLink.nextSet(t); !up {
		t.Errorf("client link SetAdminUp(%t) after host1 up, want true", up)
	}
	// Setting the client link up reports its carrier (host2 up) back to the
	// server, which re-asserts UP; that is idempotent.
	if up := serverLink.nextSet(t); !up {
		t.Errorf("server link SetAdminUp(%t) after client link up, want true", up)
	}

	// host2 goes down: the server link is set admin down.
	clientLink.setPeerUp(false)
	if up := serverLink.nextSet(t); up {
		t.Errorf("server link SetAdminUp(%t) after host2 down, want false", up)
	}
	serverLink.noSet(t)
	clientLink.noSet(t)
}
