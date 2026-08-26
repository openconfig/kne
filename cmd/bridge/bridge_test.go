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
}

func TestRunBridgeCancel(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())

	errCh := make(chan error, 1)
	go func() {
		// Use port 0 or a high unused test port; here we pass port 0 so the OS assigns an ephemeral port,
		// or pass 0 to test listener binding & cancellation.
		errCh <- runBridge(ctx, 0)
	}()

	// Allow server to spin up, then cancel context to trigger clean shutdown
	time.Sleep(50 * time.Millisecond)
	cancel()

	select {
	case err := <-errCh:
		if err != nil {
			t.Fatalf("runBridge returned error on context cancellation: %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatalf("runBridge did not terminate after context cancellation")
	}
}
