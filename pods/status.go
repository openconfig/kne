package pods

import (
	"context"
	"fmt"
	"io"
	"os"
	"strings"
	"sync"
	"time"

	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/kubernetes"
	log "k8s.io/klog/v2"
)

// A Watcher watches pod and container updates.
type Watcher struct {
	ctx       context.Context
	errCh     chan error
	wstop     func()
	cancel    func()
	podStates map[types.UID]string
	cStates   map[string]string
	ch        chan *PodStatus
	stdout    io.Writer
	warningf  func(string, ...any)

	mu               sync.Mutex
	progress         bool
	currentNamespace string
	currentPod       types.UID
}

// NewWatcher returns a Watcher on the provided client or an error.  The cancel
// function is called when the Watcher determines a container has permanently
// failed.  The Watcher will exit if the context provided is canceled, an error
// is encountered, or Cleanup is called.
func NewWatcher(ctx context.Context, client kubernetes.Interface, cancel func()) (*Watcher, error) {
	ch, stop, err := WatchPodStatus(ctx, client, "")
	if err != nil {
		return nil, err
	}
	w := newWatcher(ctx, cancel, ch, stop)
	go w.watch()
	return w, nil
}

func newWatcher(ctx context.Context, cancel func(), ch chan *PodStatus, stop func()) *Watcher {
	w := &Watcher{
		ctx:       ctx,
		ch:        ch,
		wstop:     stop,
		cancel:    cancel,
		stdout:    os.Stdout,
		podStates: map[types.UID]string{},
		cStates:   map[string]string{},
		warningf:  log.Warningf,
	}
	// A channel is used to record errors from the watcher to prevent any
	// possible race conditions if Cleanup is called while an update is
	// happening.  At most one error will be written to the channel.
	w.errCh = make(chan error, 1)
	w.display("Displaying state changes for pods and containers")
	return w
}

// SetProgress determins if progress output should be displayed while watching.
func (w *Watcher) SetProgress(value bool) {
	w.mu.Lock()
	w.progress = value
	w.mu.Unlock()
}

func (w *Watcher) stop() {
	w.mu.Lock()
	stop := w.wstop
	w.wstop = nil
	w.mu.Unlock()
	if stop != nil {
		stop()
	}
}

// Cleanup should be called when the Watcher is no longer needed.  If the
// Watcher encountered an error the provided err is logged and the Watcher error
// is returned, otherwise err is returned.
func (w *Watcher) Cleanup(err error) error {
	w.stop()
	select {
	case werr := <-w.errCh:
		if err != nil {
			w.warningf("Deploy() failed: %v", err)
		}
		w.warningf("Deployment failed: %v", werr)
		return werr
	default:
	}
	return err
}

func (w *Watcher) watch() {
	defer w.stop()
	for {
		select {
		case s, ok := <-w.ch:
			if !ok || !w.updatePod(s) {
				return
			}
		case <-w.ctx.Done():
			return
		}
	}
}

var timeNow = func() string { return time.Now().Format("15:04:05 ") }

func (w *Watcher) display(format string, v ...any) {
	if w.progress {
		fmt.Fprintf(w.stdout, timeNow()+format+"\n", v...)
	}
}

func (w *Watcher) updatePod(s *PodStatus) bool {
	newNamespace := s.Namespace != w.currentNamespace
	var newState string

	if s.Ready {
		newState = "READY"
	} else {
		switch s.Phase {
		case PodPending:
			newState = "pending"
		case PodRunning:
			newState = "running"
		case PodSucceeded:
			newState = "success"
		case PodFailed:
			newState = "failed"
		}
	}

	showPodState := func(s *PodStatus, oldState, newState string) {
		if w.currentPod == s.UID && oldState == newState {
			return
		}
		w.currentPod = s.UID
		if newNamespace {
			w.currentNamespace = s.Namespace
			w.display("NS: %s", s.Namespace)
			newNamespace = false
		}
		if newState == "" {
			w.display("    POD: %s", s.Name)
		} else {
			w.display("    POD: %s is now %s", s.Name, newState)
		}
	}
	showContainer := func(s *PodStatus, c *ContainerStatus, state string) {
		id := s.Namespace + ":" + s.Name + ":" + c.Name
		if oldState := w.cStates[id]; oldState != state {
			showPodState(s, "", "")
			w.cStates[id] = state
			w.display("         CONTAINER: %s is now %s", c.Name, state)
		}
	}

	if oldState := w.podStates[s.UID]; oldState != newState {
		showPodState(s, oldState, newState)
		w.podStates[s.UID] = newState
	}
	if newState == "failed" {
		w.errCh <- fmt.Errorf("Pod %s failed to deploy", s.Name)
		w.cancel()
		return false
	}

	for _, c := range append(s.Containers, s.InitContainers...) {
		c := c // because of lint
		fullName := fmt.Sprintf("NS:%s POD:%s CONTAINER:%s", s.Namespace, s.Name, c.Name)
		// We could keep track of all container
		// states and issue a message each time
		// the state changes
		if c.Ready {
			showContainer(s, &c, "READY")
			continue
		}
		s.Ready = false // This should never happen
		switch {
		case c.Reason == "ErrImagePull" && strings.Contains(c.Message, "code = NotFound"):
			showContainer(s, &c, "FAILED")
			w.warningf("%s: %s", fullName, c.Message)
			w.errCh <- fmt.Errorf("%s IMAGE:%s not found", fullName, c.Image)
			w.cancel()
			return false
		case c.Reason == "ErrImagePull":
			showContainer(s, &c, c.Reason)
			log.Infof("%s in ErrImagePull", fullName)
		case c.Reason == "ImagePullBackOff":
			showContainer(s, &c, c.Reason)
			log.Infof("%s in PullBackoff", fullName)
		default:
			showContainer(s, &c, c.Reason)
		}
	}
	return true
}
