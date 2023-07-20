package events

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strings"
	"testing"

	"github.com/h-fam/errdiff"
	kfake "k8s.io/client-go/kubernetes/fake"
)

func init() {
	_ = timeNow()
	timeNow = func() string { return "01:23:45 " }
}

func TestDisplay(t *testing.T) {
	var buf strings.Builder
	var w Watcher
	w.stdout = &buf
	w.SetProgress(false)
	w.display("hello %s", "world")
	if got := buf.String(); got != "" {
		t.Errorf("display w/o progress got %q, want \"\"", got)
	}
	buf.Reset()
	w.SetProgress(true)
	w.display("hello %s", "world")
	want := "01:23:45 hello world\n"
	got := buf.String()
	if got != want {
		t.Errorf("display got %q, want %q", got, want)
	}
}

func TestStop(t *testing.T) {
	canceled := false
	cancel := func() { canceled = true }
	stopped := false
	stop := func() { stopped = true }
	newWatcher(context.TODO(), cancel, nil, stop).stop()
	if stopped != true {
		t.Errorf("got stopped %v, want %v", stopped, true)
	}
	if canceled != false {
		t.Errorf("got canceled %v, want %v", canceled, false)
	}
}

func TestCleanup(t *testing.T) {
	var (
		error1 = errors.New("First Error")
		error2 = errors.New("Second Error")
	)
	for _, tt := range []struct {
		name     string
		err      error
		werr     error
		want     error
		canceled bool
		output   string
	}{
		{
			name: "no_errors",
		},
		{
			name: "passed_error",
			err:  error1,
			want: error1,
		},
		{
			name:   "generated_error",
			werr:   error2,
			want:   error2,
			output: "Deployment failed: Second Error\n",
		},
		{
			name:   "passed_and_generated_error",
			err:    error1,
			werr:   error2,
			want:   error2,
			output: "Deploy() failed: First Error\nDeployment failed: Second Error\n",
		},
	} {
		t.Run(tt.name, func(t *testing.T) {
			var buf strings.Builder
			canceled := false
			cancel := func() { canceled = true }
			stopped := false
			stop := func() { stopped = true }
			w := newWatcher(context.TODO(), cancel, nil, stop)
			w.stdout = &buf
			if tt.werr != nil {
				w.errCh <- tt.werr
			}
			w.warningf = func(f string, v ...any) {
				fmt.Fprintf(&buf, f+"\n", v...)
			}
			got := w.Cleanup(tt.err)
			if s := errdiff.Check(got, tt.want); s != "" {
				t.Errorf("%s", s)
			}
			if stopped != true {
				t.Errorf("got stopped %v, want %v", stopped, true)
			}
			if canceled != false {
				t.Errorf("got canceled %v, want %v", canceled, false)
			}
			output := buf.String()
			if output != tt.output {
				t.Errorf("Got output %q, want %q", output, tt.output)
			}
		})
	}
}

func TestWatcher(t *testing.T) {
	normalEvent := &EventStatus{Name: "event_name", UID: "uid", Namespace: "ns", Message: "Created container kube-rbac-proxy", Type: EventNormal}
	insufficientCPU := &EventStatus{Name: "event_name", UID: "uid", Namespace: "ns", Message: "0/1 nodes are available: 1 Insufficient cpu. preemption: 0/1 nodes are available: 1 No preemption victims found for incoming pod..", Type: EventWarning}
	insufficientMem := &EventStatus{Name: "event_name", UID: "uid", Namespace: "ns", Message: "0/1 nodes are available: 1 Insufficient memory. preemption: 0/1 nodes are available: 1 No preemption victims found for incoming pod..", Type: EventWarning}
	for _, tt := range []struct {
		name     string
		s        *EventStatus
		output   string
		closed   bool
		canceled bool
	}{
		{
			name:   "no_updates",
			closed: true,
		},
		{
			name:   "all",
			s:      normalEvent,
			closed: true,
			output: `
01:23:45 NS: ns
01:23:45 Event name: event_name
01:23:45 EventType: Normal
01:23:45 Event message: Created container kube-rbac-proxy
`[1:],
		},
		{
			name:   "failed",
			s:      insufficientCPU,
			closed: true,
			output: `
01:23:45 NS: ns
01:23:45 Event name: event_name
01:23:45 EventType: Warning
01:23:45 Event message: 0/1 nodes are available: 1 Insufficient cpu. preemption: 0/1 nodes are available: 1 No preemption victims found for incoming pod..
`[1:],
		},
		{
			name:   "failed",
			s:      insufficientMem,
			closed: true,
			output: `
01:23:45 NS: ns
01:23:45 Event name: event_name
01:23:45 EventType: Warning
01:23:45 Event message: 0/1 nodes are available: 1 Insufficient memory. preemption: 0/1 nodes are available: 1 No preemption victims found for incoming pod..
`[1:],
		},
		{
			name:     "canceled",
			canceled: true,
		},
	} {
		t.Run(tt.name, func(t *testing.T) {
			ctx, cancel := context.WithCancel(context.TODO())
			stopped := false
			stop := func() { stopped = true }
			ch := make(chan *EventStatus, 2)
			var buf strings.Builder
			w := newWatcher(ctx, cancel, ch, stop)
			w.progress = true
			w.stdout = &buf
			if tt.s != nil {
				ch <- tt.s
			}
			if tt.closed {
				close(ch)
			}
			if tt.canceled {
				cancel()
			}
			w.watch()
			if !stopped {
				t.Errorf("Watcher did not stop")
			}
			if output := buf.String(); output != tt.output {
				t.Errorf("Got output %q, want %q", output, tt.output)
			}
		})
	}
}

func TestNewWatcher(t *testing.T) {
	if _, err := NewWatcher(context.TODO(), nil, func() {}); err == nil {
		t.Errorf("NewWatcher did not return an error on bad input")
	}
	client := kfake.NewSimpleClientset()
	ctx, cancel := context.WithCancel(context.TODO())
	defer cancel()
	w, err := NewWatcher(ctx, client, func() {})
	if err != nil {
		t.Errorf("NewWatcher failed: %v", err)
	}
	if w.ctx != ctx {
		t.Errorf("Watcher has the wrong context")
	}
	if w.ch == nil {
		t.Errorf("Watcher has no channel")
	}
	if w.wstop == nil {
		t.Errorf("Watcher has no stop")
	}
	if w.cancel == nil {
		t.Errorf("Watcher has no cancel")
	}
	if w.stdout != os.Stdout {
		t.Errorf("Watcher's stdout is not os.Stdout")
	}
	if w.warningf == nil {
		t.Errorf("Watcher's warningf is nil")
	}
	if w.eventStates == nil {
		t.Errorf("Watcher did not make eventStates")
	}
}
