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
	"testing"
	"time"

	"github.com/openconfig/kne/bridge"
)

func TestNew(t *testing.T) {
	cmd := New()
	if cmd.Use != "bridge" {
		t.Errorf("got Use = %q, want %q", cmd.Use, "bridge")
	}

	flag := cmd.Flag("listen_port")
	if flag == nil {
		t.Fatalf("missing --listen_port flag")
	}
	if flag.DefValue != "50058" {
		t.Errorf("got default listen_port = %q, want %q", flag.DefValue, "50058")
	}

	peerFlag := cmd.Flag("peer")
	if peerFlag == nil {
		t.Fatalf("missing --peer flag")
	}
}

func TestRunServerCancel(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())

	errCh := make(chan error, 1)
	go func() {
		errCh <- runServer(ctx, 0)
	}()

	time.Sleep(50 * time.Millisecond)
	cancel()

	select {
	case err := <-errCh:
		if err != nil {
			t.Fatalf("runServer returned error on context cancellation: %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatalf("runServer did not terminate after context cancellation")
	}
}

func TestRunClientCancel(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())

	errCh := make(chan error, 1)
	go func() {
		errCh <- runClient(ctx, bridge.ClientConfig{
			PeerAddress:    "nonexistent:50058",
			LocalInterface: "eth1",
			RetryInterval:  10 * time.Millisecond,
		})
	}()

	time.Sleep(50 * time.Millisecond)
	cancel()

	select {
	case err := <-errCh:
		if err != nil {
			t.Fatalf("runClient returned error on context cancellation: %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatalf("runClient did not terminate after context cancellation")
	}
}

func TestClientSubcommandMissingPeer(t *testing.T) {
	cmd := New()
	cmd.SetArgs([]string{"client"})
	err := cmd.Execute()
	if err == nil {
		t.Fatalf("Expected error when running client without --peer, got nil")
	}
}

func TestPersistentPreRunE(t *testing.T) {
	cmd := New()
	if err := cmd.PersistentPreRunE(cmd, []string{}); err != nil {
		t.Fatalf("PersistentPreRunE failed: %v", err)
	}
	flag := cmd.Flags().Lookup("logtostderr")
	if flag != nil && flag.Value.String() != "true" {
		t.Errorf("got logtostderr = %q, want %q", flag.Value.String(), "true")
	}
}
