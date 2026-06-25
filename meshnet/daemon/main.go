package main

import (
	"flag"
	"os"
	"strconv"

	"github.com/networkop/meshnet-cni/daemon/cni"
	"github.com/networkop/meshnet-cni/daemon/grpcwire"
	"github.com/networkop/meshnet-cni/daemon/meshnet"
	"github.com/networkop/meshnet-cni/daemon/vxlan"
	"github.com/networkop/meshnet-cni/utils/wireutil"
	log "github.com/sirupsen/logrus"
)

func main() {

	if err := cni.Init(); err != nil {
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

	if err := m.Serve(); err != nil {
		log.Errorf("daemon exited badly: %v", err)
		os.Exit(1)
	}
}
