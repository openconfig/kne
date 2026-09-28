package main

import (
	"context"
	"flag"
	"os"
	"os/signal"
	"strconv"
	"syscall"

	"github.com/openconfig/kne/third_party/meshnet/daemon/cni"
	"github.com/openconfig/kne/third_party/meshnet/daemon/grpcwire"
	"github.com/openconfig/kne/third_party/meshnet/daemon/meshnet"
	"github.com/openconfig/kne/third_party/meshnet/daemon/vxlan"
	"github.com/openconfig/kne/third_party/meshnet/utils/wireutil"
	log "github.com/sirupsen/logrus"
)

func main() {
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()

	if err := cni.Init(ctx); err != nil {
		log.Errorf("Failed to initialise CNI plugin: %v", err)
		os.Exit(1)
	}
	defer cni.Cleanup()

	isDebug := flag.Bool("d", false, "enable degugging")
	grpcPort, err := strconv.Atoi(os.Getenv("GRPC_PORT"))
	if err != nil || grpcPort == 0 {
		grpcPort = wireutil.GRPCDefaultPort
	}
	flag.Parse()
	log.SetLevel(log.InfoLevel)
	if *isDebug {
		log.SetLevel(log.DebugLevel)
		log.Debug("Verbose logging enabled")
	}

	meshnet.InitLogger()
	grpcwire.InitLogger()
	vxlan.InitLogger()

	m, err := meshnet.New(meshnet.Config{
		Port: grpcPort,
	})
	if err != nil {
		log.Errorf("failed to create meshnet: %v", err)
		os.Exit(1)
	}
	log.Info("Starting meshnet daemon...with grpc support")

	grpcwire.SetGWireClient(m.GWireDynClient)

	// read grpcwire info (if any) from data store and update local db
	err = grpcwire.ReconGWires()
	if err != nil {
		log.Errorf("could not reconcile grpc wire: %v", err)
		// generate error and continue
	}

	go m.RunControllerLoop(ctx)

	if err := m.Serve(); err != nil {
		log.Errorf("daemon exited badly: %v", err)
		os.Exit(1)
	}
}
