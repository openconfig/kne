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
	"os/signal"
	"syscall"
	"time"

	"github.com/openconfig/kne/bridge"
	wpb "github.com/openconfig/kne/proto/wire"
	"github.com/spf13/cobra"
	"google.golang.org/grpc"
	log "k8s.io/klog/v2"
)

// New returns the bridge subcommand.
func New() *cobra.Command {
	var (
		listenPort      int
		peerAddress     string
		localInterface  string
		remoteInterface string
		retryInterval   time.Duration
	)

	var bridgeCmd *cobra.Command
	bridgeCmd = &cobra.Command{
		Use:     "bridge",
		Aliases: []string{"packet-bridge", "packet_bridge"},
		Short:   "Start the KNE packet bridge daemon in server or client mode",
		PersistentPreRunE: func(cmd *cobra.Command, args []string) error {
			// Default daemon logs to stderr so container logs and kubectl logs work out-of-the-box.
			if f := cmd.Flags().Lookup("logtostderr"); f != nil && !f.Changed {
				_ = f.Value.Set("true")
			}
			for p := cmd.Parent(); p != nil; p = p.Parent() {
				if p != bridgeCmd && p.PersistentPreRunE != nil {
					return p.PersistentPreRunE(cmd, args)
				}
			}
			return nil
		},
		RunE: func(cmd *cobra.Command, args []string) error {
			if peerAddress != "" {
				return runClient(cmd.Context(), bridge.ClientConfig{
					PeerAddress:     peerAddress,
					LocalInterface:  localInterface,
					RemoteInterface: remoteInterface,
					RetryInterval:   retryInterval,
				})
			}
			return runServer(cmd.Context(), listenPort)
		},
	}

	bridgeCmd.Flags().IntVar(&listenPort, "listen_port", 50058, "TCP port to listen for incoming gRPC Wire streaming connections (server mode)")
	bridgeCmd.Flags().StringVar(&peerAddress, "peer", "", "Remote bridge server host:port to connect to (client mode)")
	bridgeCmd.Flags().StringVarP(&localInterface, "interface", "i", "eth1", "Local network interface to bridge (client mode)")
	bridgeCmd.Flags().StringVar(&remoteInterface, "remote_interface", "", "Remote interface name on peer bridge server (client mode, defaults to --interface)")
	bridgeCmd.Flags().DurationVar(&retryInterval, "retry_interval", 2*time.Second, "Reconnect retry delay when disconnected in client mode")

	serverCmd := &cobra.Command{
		Use:   "server",
		Short: "Run the packet bridge as a gRPC Wire server",
		RunE: func(cmd *cobra.Command, args []string) error {
			return runServer(cmd.Context(), listenPort)
		},
	}
	serverCmd.Flags().IntVar(&listenPort, "listen_port", 50058, "TCP port to listen for incoming gRPC Wire streaming connections")

	clientCmd := &cobra.Command{
		Use:   "client",
		Short: "Run the packet bridge as a client connected to a remote bridge server",
		RunE: func(cmd *cobra.Command, args []string) error {
			if peerAddress == "" {
				return fmt.Errorf("--peer flag is required in client mode")
			}
			return runClient(cmd.Context(), bridge.ClientConfig{
				PeerAddress:     peerAddress,
				LocalInterface:  localInterface,
				RemoteInterface: remoteInterface,
				RetryInterval:   retryInterval,
			})
		},
	}
	clientCmd.Flags().StringVar(&peerAddress, "peer", "", "Remote bridge server host:port to connect to (required)")
	clientCmd.Flags().StringVarP(&localInterface, "interface", "i", "eth1", "Local network interface to bridge")
	clientCmd.Flags().StringVar(&remoteInterface, "remote_interface", "", "Remote interface name on peer bridge server (defaults to --interface)")
	clientCmd.Flags().DurationVar(&retryInterval, "retry_interval", 2*time.Second, "Reconnect retry delay when disconnected")

	bridgeCmd.AddCommand(serverCmd)
	bridgeCmd.AddCommand(clientCmd)

	return bridgeCmd
}

func runServer(ctx context.Context, listenPort int) error {
	addr := fmt.Sprintf(":%d", listenPort)
	lis, err := net.Listen("tcp", addr)
	if err != nil {
		return fmt.Errorf("failed to listen on %s: %w", addr, err)
	}
	log.Infof("KNE Packet Bridge Server listening on %s", addr)

	bridgeCtx, stop := signal.NotifyContext(ctx, syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	bridgeServer := bridge.NewServer(bridgeCtx)
	defer func() {
		_ = bridgeServer.Close()
	}()

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

func runClient(ctx context.Context, cfg bridge.ClientConfig) error {
	bridgeCtx, stop := signal.NotifyContext(ctx, syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	client, err := bridge.NewClient(cfg)
	if err != nil {
		return err
	}

	log.Infof("Starting KNE Packet Bridge Client (local: %s, peer: %s)...", cfg.LocalInterface, cfg.PeerAddress)
	if err := client.Run(bridgeCtx); err != nil && bridgeCtx.Err() == nil {
		return err
	}

	return nil
}
