package pods

import (
	"context"
	"testing"

	"github.com/google/go-cmp/cmp"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/runtime/serializer"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	"k8s.io/apimachinery/pkg/watch"
	kfake "k8s.io/client-go/kubernetes/fake"
	ktest "k8s.io/client-go/testing"
)

func newClient() *kfake.Clientset {
	client := kfake.NewSimpleClientset()
	scheme := runtime.NewScheme()
	codecs := serializer.NewCodecFactory(scheme)
	_ = codecs

	metav1.AddToGroupVersion(scheme, schema.GroupVersion{Version: "v1"})
	utilruntime.Must(kfake.AddToScheme(scheme))
	return client
}

func TestGetPodStatus(t *testing.T) {
	client := newClient()
	var podUpdates = [][]*corev1.Pod{
		{ceos1pod, ixia1pod},
		{ceos2pod, ixia2pod},
		{ceos3pod, ixia3pod},
		{ceos4pod},
		{ceos5pod},
		{ceos6pod},
		{ceos7pod},
		{meshnet1pod},
	}

	var podGetUpdatesStatus = [][]PodStatus{
		{ceos1status, ixia1status},
		{ceos2status, ixia2status},
		{ceos3status, ixia3status},
		{ceos4status},
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
	client := newClient()

	var wanted = []*PodStatus{
		&ceos1status,
		&ceos2status,
		&ceos3status,
		&ceos4status,
		&ceos5status,
		&ceos6status,
		&ceos7status,
	}
	var updates = []*corev1.Pod{
		ceos1pod,
		ceos2pod,
		ceos3pod,
		ceos4pod,
		ceos5pod,
		ceos6pod,
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
	for _, want := range wanted {
		got, ok := <-ch
		if !ok {
			t.Fatalf("channel closed early")
		}
		if !got.Equal(want) {
			t.Fatalf("\ngot : %v\nwant: %v", got, want)
		}
	}
}
