// Package events provides events status for a namespace using kubectl.
package events

import (
	"context"
	"errors"
	"fmt"
	"strings"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/kubernetes"
)

// A EventStatus represents the status of a single Event.
type EventStatus struct {
	Name      string // name of event
	UID       types.UID
	Namespace string
	Message   string
	Type      string
	Event     corev1.Event // copy of the raw event
}

func (e *EventStatus) String() string {
	var buf strings.Builder
	add := func(k, v string) {
		if v != "" {
			fmt.Fprintf(&buf, ", %s: %q", k, v)
		}
	}
	fmt.Fprintf(&buf, "{Name: %q", e.Name)
	add("UID", string(e.UID))
	add("Namespace", e.Namespace)
	return buf.String()
}

func (e *EventStatus) Equal(q *EventStatus) bool {
	if e.UID != q.UID ||
		e.Name != q.Name ||
		e.Namespace != q.Namespace {
		return false
	}
	return true
}

// Values for EventType.  These constants are copied from k8s.io/api/core/v1 as a
// convenience.
const (
	EventNormal  = corev1.EventTypeNormal
	EventWarning = corev1.EventTypeWarning
)

// GetEventStatus returns the status of the events found in the supplied namespace.
func GetEventStatus(ctx context.Context, client kubernetes.Interface, namespace string) ([]*EventStatus, error) {
	events, err := client.CoreV1().Events(namespace).List(ctx, metav1.ListOptions{})
	if err != nil {
		return nil, err
	}
	var statuses []*EventStatus
	for _, event := range events.Items {
		event := event
		statuses = append(statuses, EventToStatus(&event))
	}
	return statuses, nil
}

// WatchEventStatus returns a channel on which the status of the events in the
// supplied namespace are written.
func WatchEventStatus(ctx context.Context, client kubernetes.Interface, namespace string) (chan *EventStatus, func(), error) {
	if ctx == nil {
		return nil, nil, errors.New("WatchEventStatus: nil context ")
	}
	if client == nil {
		return nil, nil, errors.New("WatchEventStatus: nil client ")
	}
	w, err := client.CoreV1().Events(namespace).Watch(ctx, metav1.ListOptions{})
	if err != nil {
		return nil, nil, err
	}
	kch := w.ResultChan()
	ch := make(chan *EventStatus, 2)
	initialTimestamp := metav1.Now()

	go func() {
		defer close(ch)
		for event := range kch {
			switch e := event.Object.(type) {
			case *corev1.Event:
				if !e.CreationTimestamp.Before(&initialTimestamp) {
					s := EventToStatus(e)
					ch <- s
				}
			}
		}
	}()
	return ch, w.Stop, nil
}

// EventToStatus returns a pointer to a new EventStatus for an event.
func EventToStatus(event *corev1.Event) *EventStatus {
	s := EventStatus{
		Name:      event.ObjectMeta.Name,
		Namespace: event.ObjectMeta.Namespace,
		UID:       event.ObjectMeta.UID,
	}
	event.DeepCopyInto(&s.Event)
	event = &s.Event
	s.Type = event.DeepCopy().Type
	s.Message = event.DeepCopy().Message
	return &s
}
