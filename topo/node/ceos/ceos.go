package ceos

import (
	"context"
	"fmt"
	"io"
	"io/ioutil"
	"regexp"
	"time"

	expect "github.com/google/goexpect"
	topopb "github.com/google/kne/proto/topo"
	"github.com/google/kne/topo/node"
	log "github.com/sirupsen/logrus"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/proto"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/fields"
)

func New(pb *topopb.Node) (node.Implementation, error) {
	cfg := defaults(pb)
	proto.Merge(cfg, pb)
	node.FixServices(cfg)
	return &Node{
		pb: cfg,
	}, nil
}

type Node struct {
	pb *topopb.Node
}

func (n *Node) Proto() *topopb.Node {
	return n.pb
}

var (
	spawner = defaultSpawner
)

func defaultSpawner(command string, timeout time.Duration, opts ...expect.Option) (expect.Expecter, <-chan error, error) {
	return expect.Spawn(command, timeout, opts...)
}

func (n *Node) GenerateSelfSigned(ctx context.Context, ni node.Interface) error {
	selfSigned := n.pb.GetConfig().GetCert().GetSelfSigned()
	if selfSigned == nil {
		log.Infof("%s - no cert config", n.pb.Name)
		return nil
	}
	log.Infof("%s - generating self signed certs", n.pb.Name)
	log.Infof("%s - waiting for pod to be running", n.pb.Name)
	w, err := ni.KubeClient().CoreV1().Pods(ni.Namespace()).Watch(ctx, metav1.ListOptions{
		FieldSelector: fields.SelectorFromSet(fields.Set{metav1.ObjectNameField: n.pb.Name}).String(),
	})
	if err != nil {
		return err
	}
	for e := range w.ResultChan() {
		p := e.Object.(*corev1.Pod)
		if p.Status.Phase == corev1.PodRunning {
			break
		}
	}
	log.Infof("%s - pod running.", n.pb.Name)
	done := false
	var g expect.Expecter
	waitTime := 0 * time.Second
	log.Infof("%s - waiting on container to be ready", n.pb.Name)
	for !done {
		time.Sleep(waitTime)
		cmd := fmt.Sprintf("kubectl exec -it -n %s %s -- Cli", ni.Namespace(), n.pb.Name)
		var err error
		g, _, err = spawner(cmd, -1)
		// This could be set per error case but probably not worth it.
		waitTime = 10 * time.Second
		if err != nil {
			log.Debugf("%s - process not ready - waiting.", n.pb.Name)
			continue
		}
		_, _, err = g.Expect(regexp.MustCompile(`>`), -1)
		if err != nil {
			log.Debugf("%s - os not ready - waiting.", n.pb.Name)
			continue
		}
		log.Debugf("%s - captured prompt", n.pb.Name)
		if err := g.Send("enable\n"); err != nil {
			return err
		}
		_, _, err = g.Expect(regexp.MustCompile(`#`), 5*time.Second)
		if err != nil {
			log.Debugf("%s - auth not ready - waiting.", n.pb.Name)
			continue
		}
		done = true
	}
	cmd := fmt.Sprintf("security pki key generate rsa %d %s\n", selfSigned.KeySize, selfSigned.KeyName)
	log.Debugf("%s - enabled - sending %q", n.pb.Name, cmd)
	if err := g.Send(cmd); err != nil {
		return err
	}
	_, _, err = g.Expect(regexp.MustCompile(`#`), 30*time.Second)
	if err != nil {
		return err
	}
	cmd = fmt.Sprintf("security pki certificate generate self-signed %s key %s parameters common-name %s\n", selfSigned.CertName, selfSigned.KeyName, n.pb.Name)
	log.Debugf("key generated - sending %q", cmd)
	if err := g.Send(cmd); err != nil {
		return err
	}
	_, _, err = g.Expect(regexp.MustCompile(`(.*)#`), 30*time.Second)
	if err != nil {
		return err
	}
	log.Infof("%s - finshed cert generation", n.pb.Name)
	return g.Close()
}

func (n *Node) ConfigPush(ctx context.Context, ns string, r io.Reader) error {
	log.Infof("Pushing config to %s:%s", ns, n.pb.Name)
	config, err := ioutil.ReadAll(r)
	log.Debug(string(config))
	if err != nil {
		return err
	}
	cmd := fmt.Sprintf("kubectl exec -it -n %s %s -- Cli", ns, n.pb.Name)
	g, _, err := spawner(cmd, -1)
	if err != nil {
		return err
	}
	_, _, err = g.Expect(regexp.MustCompile(`>`), -1)
	if err != nil {
		return err
	}
	if err := g.Send("enable\n"); err != nil {
		return err
	}
	_, _, err = g.Expect(regexp.MustCompile(`#`), -1)
	if err != nil {
		return err
	}
	if err := g.Send("configure terminal\n"); err != nil {
		return err
	}
	_, _, err = g.Expect(regexp.MustCompile(`\(config\)#`), -1)
	if err != nil {
		return err
	}
	if err := g.Send(string(config)); err != nil {
		return err
	}
	_, _, err = g.Expect(regexp.MustCompile(`#`), -1)
	if err != nil {
		return err
	}
	log.Info("Finshed config push")
	return g.Close()
}

func (n *Node) CreateNodeResource(_ context.Context, _ node.Interface) error {
	return status.Errorf(codes.Unimplemented, "Unimplemented")
}

func (n *Node) DeleteNodeResource(_ context.Context, _ node.Interface) error {
	return status.Errorf(codes.Unimplemented, "Unimplemented")
}

func defaults(pb *topopb.Node) *topopb.Node {
	if pb == nil {
		pb = &topopb.Node{
			Name: "default_ceos_node",
		}
	}
	return &topopb.Node{
		Constraints: map[string]string{
			"cpu":    "0.5",
			"memory": "1Gi",
		},
		Services: map[uint32]*topopb.Service{
			443: &topopb.Service{
				Name:    "ssl",
				Inside:  443,
				Outside: node.GetNextPort(),
			},
			22: &topopb.Service{
				Name:    "ssh",
				Inside:  22,
				Outside: node.GetNextPort(),
			},
			6030: &topopb.Service{
				Name:    "gnmi",
				Inside:  6030,
				Outside: node.GetNextPort(),
			},
		},
		Labels: map[string]string{
			"type": topopb.Node_ARISTA_CEOS.String(),
		},
		Config: &topopb.Config{
			Image: "ceos:latest",
			Command: []string{
				"/sbin/init",
				"systemd.setenv=INTFTYPE=eth",
				"systemd.setenv=ETBA=1",
				"systemd.setenv=SKIP_ZEROTOUCH_BARRIER_IN_SYSDBINIT=1",
				"systemd.setenv=CEOS=1",
				"systemd.setenv=EOS_PLATFORM=ceoslab",
				"systemd.setenv=container=docker",
			},
			Env: map[string]string{
				"CEOS":                                "1",
				"EOS_PLATFORM":                        "ceoslab",
				"container":                           "docker",
				"ETBA":                                "1",
				"SKIP_ZEROTOUCH_BARRIER_IN_SYSDBINIT": "1",
				"INTFTYPE":                            "eth",
			},
			EntryCommand: fmt.Sprintf("kubectl exec -it %s -- Cli", pb.Name),
			ConfigPath:   "/mnt/flash",
			ConfigFile:   "startup-config",
		},
	}
}

func init() {
	node.Register(topopb.Node_ARISTA_CEOS, New)
}
