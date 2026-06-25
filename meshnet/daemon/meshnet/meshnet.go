package meshnet

import (
	"fmt"
	"io"
	"net"
	"os"
	"path/filepath"

	grpc_middleware "github.com/grpc-ecosystem/go-grpc-middleware"
	grpc_ctxtags "github.com/grpc-ecosystem/go-grpc-middleware/tags"

	glogrus "github.com/grpc-ecosystem/go-grpc-middleware/logging/logrus"
	log "github.com/sirupsen/logrus"
	"google.golang.org/grpc"
	"google.golang.org/grpc/grpclog"
	"google.golang.org/grpc/reflection"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"
	"k8s.io/client-go/util/homedir"

	topologyclientv1 "github.com/networkop/meshnet-cni/api/clientset/v1beta1"
	"github.com/networkop/meshnet-cni/utils/wireutil"

	mpb "github.com/networkop/meshnet-cni/daemon/proto/meshnet/v1beta1"
)

type Config struct {
	Port     int
	GRPCOpts []grpc.ServerOption
}

type Meshnet struct {
	mpb.UnimplementedLocalServer
	mpb.UnimplementedRemoteServer
	mpb.UnimplementedWireProtocolServer
	config         Config
	kClient        kubernetes.Interface
	tClient        topologyclientv1.Interface
	GWireDynClient *dynamic.DynamicClient
	rCfg           *rest.Config
	s              *grpc.Server
	lis            net.Listener
}

var mnetdLogger *log.Entry = nil

func InitLogger() {
	mnetdLogger = log.WithFields(log.Fields{"daemon": "meshnetd"})
}

func restConfig() (*rest.Config, error) {
	mnetdLogger.Infof("Trying in-cluster configuration")
	rCfg, err := rest.InClusterConfig()
	if err != nil {
		kubecfg := filepath.Join(".kube", "config")
		if home := homedir.HomeDir(); home != "" {
			kubecfg = filepath.Join(home, kubecfg)
		}
		mnetdLogger.Infof("Falling back to kubeconfig: %q", kubecfg)
		rCfg, err = clientcmd.BuildConfigFromFlags("", kubecfg)
		if err != nil {
			mnetdLogger.Infof("error in Falling back to kubeconfig: %v", err)
			return nil, err
		}
	}
	return rCfg, nil
}

func New(cfg Config) (*Meshnet, error) {
	rCfg, err := restConfig()
	if err != nil {
		return nil, err
	}
	kClient, err := kubernetes.NewForConfig(rCfg)
	if err != nil {
		return nil, err
	}
	tClient, err := topologyclientv1.NewForConfig(rCfg)
	if err != nil {
		return nil, err
	}
	gwireDynClient, err := dynamic.NewForConfig(rCfg)
	if err != nil {
		return nil, err
	}

	lis, err := net.Listen("tcp", fmt.Sprintf(":%d", cfg.Port))
	if err != nil {
		return nil, err
	}
	// If the link type is GRPC then set the GRPC logging level to LevelNone
	// Otherwise there will be GRPC log for every packet sent as for link type GRPC, GRPC is also the data-plane. This is too
	// much of log that does not help in debugging and K8S does log rotation very frequently.
	var svr *grpc.Server
	lnkTyp := os.Getenv("INTER_NODE_LINK_TYPE")
	if lnkTyp == wireutil.INTER_NODE_LINK_GRPC {
		svr = grpc.NewServer(cfg.GRPCOpts...)
	} else {
		svr = newServerWithLogging(cfg.GRPCOpts...)
	}

	m := &Meshnet{
		config:         cfg,
		rCfg:           rCfg,
		kClient:        kClient,
		tClient:        tClient,
		GWireDynClient: gwireDynClient,
		lis:            lis,
		s:              svr,
	}
	mpb.RegisterLocalServer(m.s, m)
	mpb.RegisterRemoteServer(m.s, m)
	mpb.RegisterWireProtocolServer(m.s, m)
	reflection.Register(m.s)

	// After server is registered, reduce logging if link type is GRPC
	if lnkTyp == wireutil.INTER_NODE_LINK_GRPC {
		// Stop all Info, Warning, Error
		grpclog.SetLoggerV2(grpclog.NewLoggerV2WithVerbosity(io.Discard, io.Discard, io.Discard, 0))
		// see Error/Fatal, but opt out on Info
		//grpclog.SetLoggerV2(grpclog.NewLoggerV2WithVerbosity(ioutil.Discard, ioutil.Discard, os.Stderr, 0))
		mnetdLogger.Infof("Enabled GRPC logging for Error and Fatal only. Disabled Info & Warning logs.")
	}

	return m, nil
}

func (m *Meshnet) Serve() error {
	mnetdLogger.Infof("GRPC server has started on port: %d", m.config.Port)
	return m.s.Serve(m.lis)
}

func (m *Meshnet) Stop() {
	m.s.Stop()
}

func newServerWithLogging(opts ...grpc.ServerOption) *grpc.Server {
	lEntry := log.NewEntry(log.StandardLogger())
	lOpts := []glogrus.Option{}
	glogrus.ReplaceGrpcLogger(lEntry)
	opts = append(opts,
		grpc_middleware.WithUnaryServerChain(
			grpc_ctxtags.UnaryServerInterceptor(grpc_ctxtags.WithFieldExtractor(grpc_ctxtags.CodeGenRequestFieldExtractor)),
			glogrus.UnaryServerInterceptor(lEntry, lOpts...),
		),
		grpc_middleware.WithStreamServerChain(
			grpc_ctxtags.StreamServerInterceptor(grpc_ctxtags.WithFieldExtractor(grpc_ctxtags.CodeGenRequestFieldExtractor)),
			glogrus.StreamServerInterceptor(lEntry, lOpts...),
		))
	return grpc.NewServer(opts...)
}
