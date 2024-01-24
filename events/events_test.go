package events

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/google/go-cmp/cmp"
	"github.com/openconfig/gnmi/errdiff"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/watch"
	kfake "k8s.io/client-go/kubernetes/fake"
	ktest "k8s.io/client-go/testing"
)

func TestGetEventStatus(t *testing.T) {
	client := kfake.NewSimpleClientset()
	var eventUpdates = [][]*corev1.Event{
		{normal1event},
		{warning1event},
		{warning2event},
	}

	var eventGetUpdatesStatus = [][]EventStatus{
		{normal1eventstatus},
		{warning1eventstatus},
		{warning2eventstatus},
	}

	next := -1

	client.PrependReactor("list", "events", func(action ktest.Action) (bool, runtime.Object, error) {
		next++
		if next >= len(eventUpdates) {
			return false, nil, nil
		}
		el := corev1.EventList{}
		for _, e := range eventUpdates[next] {
			el.Items = append(el.Items, *e)
		}
		return true, &el, nil
	})

	for i := range eventUpdates {
		got, err := GetEventStatus(context.Background(), client, "")
		if err != nil {
			t.Fatal(err)
		}
		want := eventGetUpdatesStatus[i]
		j := -1
		for len(got) > 0 {
			j++
			gotevent := got[0]
			got = got[1:]
			if len(want) == 0 {
				t.Errorf("%d-%d: extra event: %v", i, j, got)
				continue
			}
			wantevent := want[0]
			want = want[1:]
			if !gotevent.Equal(&wantevent) {
				t.Errorf("%d-%d FAIL:\ngot : %s\nwant: %s", i, j, gotevent.String(), wantevent.String())
			} else if false {
				t.Logf("%d-%d okay:\ngot : %s\nwant: %s", i, j, gotevent.String(), wantevent.String())
			}

			if false {
				if s := cmp.Diff(got, want); s != "" {
					t.Errorf("update %d: %s\n", i, s)
				}
			}
		}
		for len(want) > 0 {
			wantevent := want[0]
			want = want[1:]
			t.Errorf("%d: missing event: %v", i, wantevent)
		}
	}
}

func TestGetEventStatusError(t *testing.T) {
	myError := errors.New("event error")
	client := kfake.NewSimpleClientset()
	client.PrependReactor("list", "events", func(action ktest.Action) (bool, runtime.Object, error) {
		return true, nil, myError
	})

	_, err := GetEventStatus(context.Background(), client, "")
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

func TestWatchEventStatus(t *testing.T) {
	if _, _, err := WatchEventStatus(nil, nil, ""); err == nil { //nolint:all
		t.Errorf("WatchEventStatus does not return an error on a nil context.")
	}
	if _, _, err := WatchEventStatus(context.TODO(), nil, ""); err == nil {
		t.Errorf("WatchEventStatus does not return an error on a nil client.")
	}
	client := kfake.NewSimpleClientset()

	var wanted = []*EventStatus{
		&normal1eventstatus,
		&warning1eventstatus,
		&warning2eventstatus,
	}
	var updates = []*corev1.Event{
		normal1event,
		warning1event,
		warning2event,
	}
	for _, u := range updates {
		u.CreationTimestamp = metav1.NewTime(time.Now().Add(time.Minute))
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

	ch, stop, err := WatchEventStatus(context.Background(), client, "")
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

func TestWatchEventStatusError(t *testing.T) {
	client := kfake.NewSimpleClientset()
	myError := errors.New("watch error")
	client.PrependWatchReactor("*", func(action ktest.Action) (bool, watch.Interface, error) {
		f := &fakeWatch{
			ch:   make(chan watch.Event, 1),
			done: make(chan struct{}),
		}
		return true, f, myError
	})

	_, _, err := WatchEventStatus(context.Background(), client, "")
	if s := errdiff.Check(err, myError); s != "" {
		t.Error(s)
	}
}

func TestString(t *testing.T) {
	for _, tt := range []struct {
		name   string
		event  *EventStatus
		status string
	}{
		{"event1", &EventStatus{Name: "event1", Namespace: "ns", UID: "event-1"}, `{Name: "event1", UID: "event-1", Namespace: "ns"}`},
		{"event2", &normal1eventstatus, normal1eventstring},
		{"event3", &warning1eventstatus, warning1eventstring},
		{"event4", &warning2eventstatus, warning2eventstring},
	} {
		t.Run(tt.name, func(t *testing.T) {
			status := tt.event.String()
			if status != tt.status {
				t.Errorf("Got/Want:\n%s\n%s", status, tt.status)
			}
		})
	}
}

func TestEqual(t *testing.T) {
	different := &EventStatus{}
	for i, event := range []*EventStatus{&normal1eventstatus, &warning1eventstatus, &warning2eventstatus} {
		lintEvent := event
		if !event.Equal(lintEvent) {
			t.Errorf("#%d: Equal returned false on equal events", i)
		}
		if event.Equal(different) {
			t.Errorf("#%d: Equal returned true on different events", i)
		}
	}
}
