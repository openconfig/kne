package events

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strings"
	"testing"

	"github.com/openconfig/gnmi/errdiff"
	"k8s.io/apimachinery/pkg/types"
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
			output: "Event Watcher error: Second Error\n",
		},
		{
			name:   "passed_and_generated_error",
			err:    error1,
			werr:   error2,
			want:   error2,
			output: "Event Watcher failed: First Error\nEvent Watcher error: Second Error\n",
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
01:23:45 NS: ns Event name: event_name Type: Normal Message: Created container kube-rbac-proxy
`[1:],
		},
		{
			name:   "failed_insufficient_cpu",
			s:      insufficientCPU,
			closed: true,
			output: `
01:23:45 NS: ns Event name: event_name Type: Warning Message: 0/1 nodes are available: 1 Insufficient cpu. preemption: 0/1 nodes are available: 1 No preemption victims found for incoming pod..
`[1:],
		},
		{
			name:   "failed_insufficient_memory",
			s:      insufficientMem,
			closed: true,
			output: `
01:23:45 NS: ns Event name: event_name Type: Warning Message: 0/1 nodes are available: 1 Insufficient memory. preemption: 0/1 nodes are available: 1 No preemption victims found for incoming pod..
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

func TestIsEventNormal(t *testing.T) {
	var buf strings.Builder

	canceled := false
	cancel := func() { canceled = true }
	stopped := false
	stop := func() { stopped = true }

	w := newWatcher(context.TODO(), cancel, nil, stop)
	w.stdout = &buf
	w.SetProgress(true)

	var seen string

	const (
		uid1 = types.UID("uid1")
		uid2 = types.UID("uid2")
		uid3 = types.UID("uid3")
	)

	for _, tt := range []struct {
		name     string
		event    *EventStatus
		want     string
		errch    string
		stopped  bool
		canceled bool
	}{
		{
			name:  "normal",
			event: &EventStatus{Name: "event1", UID: uid1, Namespace: "ns1", Type: EventNormal, Message: "normal event"},
			want: `
01:23:45 NS: ns1 Event name: event1 Type: Normal Message: normal event
`[1:],
		},
		{
			name:  "insufficient_cpu",
			event: &EventStatus{Name: "event2", UID: uid1, Namespace: "ns1", Type: EventWarning, Message: "0/1 nodes are available: 1 Insufficient cpu. preemption: 0/1 nodes are available: 1 No preemption victims found for incoming pod.."},
			want: `
01:23:45 NS: ns1 Event name: event2 Type: Warning Message: 0/1 nodes are available: 1 Insufficient cpu. preemption: 0/1 nodes are available: 1 No preemption victims found for incoming pod..
`[1:],
			errch:    "Event failed due to  . Message: 0/1 nodes are available: 1 Insufficient cpu. preemption: 0/1 nodes are available: 1 No preemption victims found for incoming pod..",
			canceled: true,
		},
		{
			name:  "insufficient_memory",
			event: &EventStatus{Name: "event3", UID: uid1, Namespace: "ns1", Type: EventWarning, Message: "0/1 nodes are available: 1 Insufficient memory. preemption: 0/1 nodes are available: 1 No preemption victims found for incoming pod.."},
			want: `
01:23:45 NS: ns1 Event name: event3 Type: Warning Message: 0/1 nodes are available: 1 Insufficient memory. preemption: 0/1 nodes are available: 1 No preemption victims found for incoming pod..
`[1:],
			errch:    "Event failed due to  . Message: 0/1 nodes are available: 1 Insufficient memory. preemption: 0/1 nodes are available: 1 No preemption victims found for incoming pod..",
			canceled: true,
		},
	} {
		w.isEventNormal(tt.event)
		t.Run(tt.name, func(t *testing.T) {
			got := buf.String()
			if !strings.HasPrefix(got, seen) {
				t.Fatalf("got %q, wanted prefix %q", got, seen)
			}
			seen, got = got, got[len(seen):]
			if got != tt.want {
				t.Errorf("got %q, want %q", got, tt.want)
			}
			var errch string
			select {
			case err := <-w.errCh:
				errch = err.Error()
			default:
			}
			if errch != tt.errch {
				t.Errorf("got error %s, want error %s", errch, tt.errch)
			}
			if stopped != tt.stopped {
				t.Errorf("got stopped %v, want %v", stopped, tt.stopped)
			}
			stopped = false
			if canceled != tt.canceled {
				t.Errorf("got canceled %v, want %v", canceled, tt.canceled)
			}
			canceled = false
		})
	}
}
