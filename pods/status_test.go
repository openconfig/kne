package pods

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strings"
	"testing"

	"github.com/h-fam/errdiff"
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

func TestUpdatePod(t *testing.T) {
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
		pod      *PodStatus
		want     string
		errch    string
		stopped  bool
		canceled bool
	}{
		{
			name: "pending1",
			pod:  &PodStatus{Name: "pod1", UID: uid1, Namespace: "ns1", Phase: PodPending},
			want: `
01:23:45 NS: ns1
01:23:45     POD: pod1 is now pending
`[1:],
		},
		{
			name: "running1",
			pod:  &PodStatus{Name: "pod1", UID: uid1, Namespace: "ns1", Phase: PodRunning},
			want: `
01:23:45     POD: pod1 is now running
`[1:],
		},
		{
			name: "success1",
			pod:  &PodStatus{Name: "pod1", UID: uid1, Namespace: "ns1", Phase: PodSucceeded},
			want: `
01:23:45     POD: pod1 is now success
`[1:],
		},
		{
			name: "ready1",
			pod:  &PodStatus{Name: "pod1", UID: uid1, Namespace: "ns1", Ready: true, Phase: PodRunning},
			want: `
01:23:45     POD: pod1 is now READY
`[1:],
		},
		{
			name: "failed1",
			pod:  &PodStatus{Name: "pod1", UID: uid1, Namespace: "ns1", Phase: PodFailed},
			want: `
01:23:45     POD: pod1 is now failed
`[1:],
			errch:    `Pod pod1 failed to deploy`,
			canceled: true,
		},
		{
			name: "pending1a",
			pod:  &PodStatus{Name: "pod1", UID: uid1, Namespace: "ns1", Phase: PodPending},
			want: `
01:23:45     POD: pod1 is now pending
`[1:],
		},
		{
			name: "pod2",
			pod: &PodStatus{Name: "pod2", UID: uid2, Namespace: "ns2", Phase: PodPending,
				Containers: []ContainerStatus{{Name: "cont1", Reason: "pending"}},
			},
			want: `
01:23:45 NS: ns2
01:23:45     POD: pod2 is now pending
01:23:45          CONTAINER: cont1 is now pending
`[1:],
		},
		{
			name: "pod2a",
			pod: &PodStatus{Name: "pod2", UID: uid2, Namespace: "ns2", Phase: PodPending,
				Containers: []ContainerStatus{{Name: "cont1", Reason: "ImagePullBackOff"}},
			},
			want: `
01:23:45          CONTAINER: cont1 is now ImagePullBackOff
`[1:],
		},
		{
			name: "pod3",
			pod:  &PodStatus{Name: "pod3", UID: uid3, Namespace: "ns2", Phase: PodPending},
			want: `
01:23:45     POD: pod3 is now pending
`[1:],
		},
		{
			name: "pod1a",
			pod: &PodStatus{Name: "pod1", UID: uid1, Namespace: "ns1", Phase: PodPending,
				Containers: []ContainerStatus{{Name: "cont1", Ready: true}},
			},
			want: `
01:23:45 NS: ns1
01:23:45     POD: pod1
01:23:45          CONTAINER: cont1 is now READY
`[1:],
		},
		{
			name: "pod1b",
			pod: &PodStatus{Name: "pod1", UID: uid1, Namespace: "ns1", Phase: PodPending,
				Containers: []ContainerStatus{{Name: "cont1", Reason: "ErrImagePull", Message: "bad connection"}},
			},
			want: `
01:23:45          CONTAINER: cont1 is now ErrImagePull
`[1:],
		},
		{
			name: "pod1c",
			pod: &PodStatus{Name: "pod1", UID: uid1, Namespace: "ns1", Phase: PodPending,
				Containers: []ContainerStatus{{Name: "cont1", Reason: "ErrImagePull", Message: "something code = NotFound here", Image: "the_image"}},
			},
			want: `
01:23:45          CONTAINER: cont1 is now FAILED
`[1:],
			errch:    "NS:ns1 POD:pod1 CONTAINER:cont1 IMAGE:the_image not found",
			canceled: true,
		},
	} {
		w.updatePod(tt.pod)
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
	failedPod := &PodStatus{Name: "pod_name", UID: "uid", Namespace: "ns", Phase: PodFailed}
	readyPod := &PodStatus{Name: "pod_name", UID: "uid", Namespace: "ns", Ready: true}
	for _, tt := range []struct {
		name     string
		s        *PodStatus
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
			s:      readyPod,
			closed: true,
			output: `
01:23:45 NS: ns
01:23:45     POD: pod_name is now READY
`[1:],
		},
		{
			name:   "failed",
			s:      failedPod,
			closed: true,
			output: `
01:23:45 NS: ns
01:23:45     POD: pod_name is now failed
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
			ch := make(chan *PodStatus, 2)
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
	if w.podStates == nil {
		t.Errorf("Watcher did not make podStates")
	}
	if w.cStates == nil {
		t.Errorf("Watcher did not make cStates")
	}
}
