package pods

import (
	"context"
	"errors"
	"testing"

	"github.com/google/go-cmp/cmp"
	"github.com/openconfig/gnmi/errdiff"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/watch"
	kfake "k8s.io/client-go/kubernetes/fake"
	ktest "k8s.io/client-go/testing"
)

func TestGetPodStatus(t *testing.T) {
	client := kfake.NewSimpleClientset()
	var podUpdates = [][]*corev1.Pod{
		{ceos1pod, ixia1pod},
		{ceos2pod, ixia2pod},
		{ceos3pod, ixia3pod},
		{ceos5pod},
		{ceos6pod},
		{ceos7pod},
		{meshnet1pod},
	}

	var podGetUpdatesStatus = [][]PodStatus{
		{ceos1status, ixia1status},
		{ceos2status, ixia2status},
		{ceos3status, ixia3status},
		{ceos5status},
		{ceos6status},
		{ceos7status},
		{meshnet1status},
	}

	next := -1

	client.PrependReactor("list", "pods", func(action ktest.Action) (bool, runtime.Object, error) {
		next++
		if next >= len(podUpdates) {
			return false, nil, nil
		}
		pl := corev1.PodList{}
		for _, p := range podUpdates[next] {
			pl.Items = append(pl.Items, *p)
		}
		return true, &pl, nil
	})

	for i := range podUpdates {
		got, err := GetPodStatus(context.Background(), client, "")
		if err != nil {
			t.Fatal(err)
		}
		want := podGetUpdatesStatus[i]
		j := -1
		for len(got) > 0 {
			j++
			gotpod := got[0]
			got = got[1:]
			if len(want) == 0 {
				t.Errorf("%d-%d: extra pod: %v", i, j, got)
				continue
			}
			wantpod := want[0]
			want = want[1:]
			if !gotpod.Equal(&wantpod) {
				t.Errorf("%d-%d FAIL:\ngot : %s\nwant: %s", i, j, gotpod.String(), wantpod.String())
			} else if false {
				t.Logf("%d-%d okay:\ngot : %s\nwant: %s", i, j, gotpod.String(), wantpod.String())
			}

			if false {
				if s := cmp.Diff(got, want); s != "" {
					t.Errorf("update %d: %s\n", i, s)
				}
			}
		}
		for len(want) > 0 {
			wantpod := want[0]
			want = want[1:]
			t.Errorf("%d: missing pod: %v", i, wantpod)
		}
	}
}
func TestGetPodStatusError(t *testing.T) {
	myError := errors.New("pod error")
	client := kfake.NewSimpleClientset()
	client.PrependReactor("list", "pods", func(action ktest.Action) (bool, runtime.Object, error) {
		return true, nil, myError
	})

	_, err := GetPodStatus(context.Background(), client, "")
	if s := errdiff.Check(err, myError); s != "" {
		t.Error(s)
	}
}

type fakeWatch struct {
	ch   chan watch.Event
	done chan struct{}
}

func (f *fakeWatch) Stop() {
	close(f.done)
}

func (f *fakeWatch) ResultChan() <-chan watch.Event {
	return f.ch
}

func TestWatchPodStatus(t *testing.T) {
	client := kfake.NewSimpleClientset()

	var wanted = []*PodStatus{
		&ceos1status,
		&ceos2status,
		&ceos3status,
		&ceos5status,
		&ceos6status,
		&ceos6Istatus,
		&ceos7status,
	}
	var updates = []*corev1.Pod{
		ceos1pod,
		ceos1pod,
		ceos2pod,
		ceos3pod,
		ceos5pod,
		ceos6pod,
		ceos6pod,
		ceos6Ipod,
		ceos7pod,
	}
	client.PrependWatchReactor("*", func(action ktest.Action) (bool, watch.Interface, error) {
		f := &fakeWatch{
			ch:   make(chan watch.Event, 1),
			done: make(chan struct{}),
		}
		go func() {
			kind := watch.Added
			for _, u := range updates {
				select {
				case f.ch <- watch.Event{
					Type:   kind,
					Object: u,
				}:
					kind = watch.Modified
				case <-f.done:
					return
				}
			}
		}()
		return true, f, nil
	})

	ch, stop, err := WatchPodStatus(context.Background(), client, "")
	if err != nil {
		t.Fatal(err)
	}
	defer stop()
	for i, want := range wanted {
		got, ok := <-ch
		if !ok {
			t.Fatalf("channel closed early")
		}
		if !got.Equal(want) {
			t.Fatalf("#%d\ngot : %v\nwant: %v", i, got, want)
		}
	}
}

func TestWatchPodStatusError(t *testing.T) {
	client := kfake.NewSimpleClientset()
	myError := errors.New("watch error")
	client.PrependWatchReactor("*", func(action ktest.Action) (bool, watch.Interface, error) {
		f := &fakeWatch{
			ch:   make(chan watch.Event, 1),
			done: make(chan struct{}),
		}
		return true, f, myError
	})

	_, _, err := WatchPodStatus(context.Background(), client, "")
	if s := errdiff.Check(err, myError); s != "" {
		t.Error(s)
	}
}

func TestString(t *testing.T) {
	for _, tt := range []struct {
		name   string
		pod    *PodStatus
		status string
	}{
		{"ceos1", &ceos1status, ceos1string}, // No containers
		{"ceos2", &ceos2status, ceos2string}, // containers not ready
		{"ceos3", &ceos3status, ceos3string}, // containers ready
		{"ceos6", &ceos6status, ceos6string}, // init containers
	} {
		t.Run(tt.name, func(t *testing.T) {
			status := tt.pod.String()
			if status != tt.status {
				t.Errorf("Got/Want:\n%s\n%s", status, tt.status)
			}
		})
	}
}

func TestEqual(t *testing.T) {
	different := &ceos6Istatus
	for i, pod := range []*PodStatus{&ceos1status, &ceos2status, &ceos3status, &ceos6status} {
		// Lint thinks pod.Equal(pod) is suspicious.
		lintPod := pod
		if !pod.Equal(lintPod) {
			t.Errorf("#%d: Equal returned false on equal pods", i)
		}
		if pod.Equal(different) {
			t.Errorf("#%d: Equal returned true on different pods", i)
		}
	}
}
