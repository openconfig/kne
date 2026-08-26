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

// Package bridge implements the KNE CLI bridge subcommand.
package bridge

import (
	"context"
	"fmt"
	"net"
	"os"
	"os/signal"
	"syscall"

	"github.com/openconfig/kne/bridge"
	wpb "github.com/openconfig/kne/proto/wire"
	"github.com/spf13/cobra"
	"google.golang.org/grpc"
	log "k8s.io/klog/v2"
)

// New returns the bridge subcommand.
func New() *cobra.Command {
	var listenPort int

	bridgeCmd := &cobra.Command{
		Use:     "bridge",
		Aliases: []string{"packet-bridge", "packet_bridge"},
		Short:   "Start the KNE packet bridge Wire service daemon",
		RunE: func(cmd *cobra.Command, args []string) error {
			return runBridge(cmd.Context(), listenPort)
		},
	}

	bridgeCmd.Flags().IntVar(&listenPort, "listen_port", 50058, "TCP port to listen for incoming gRPC Wire streaming connections")
	return bridgeCmd
}

func runBridge(ctx context.Context, listenPort int) error {
	addr := fmt.Sprintf(":%d", listenPort)
	lis, err := net.Listen("tcp", addr)
	if err != nil {
		return fmt.Errorf("failed to listen on %s: %w", addr, err)
	}
	log.Infof("KNE Packet Bridge listening on %s", addr)

	bridgeCtx, cancel := context.WithCancel(ctx)
	defer cancel()

	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)
	go func() {
		select {
		case sig := <-sigChan:
			log.Infof("Received signal %v, initiating shutdown...", sig)
			cancel()
		case <-bridgeCtx.Done():
		}
	}()

	bridgeServer := bridge.NewServer(bridgeCtx)
	defer bridgeServer.Close()

	grpcServer := grpc.NewServer()
	wpb.RegisterWireServer(grpcServer, bridgeServer)

	go func() {
		<-bridgeCtx.Done()
		log.Infof("Stopping gRPC server...")
		grpcServer.GracefulStop()
	}()

	if err := grpcServer.Serve(lis); err != nil && bridgeCtx.Err() == nil {
		return fmt.Errorf("gRPC server failed: %w", err)
	}

	return nil
}
