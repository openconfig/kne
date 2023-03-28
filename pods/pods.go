// Package pods provides pod status for a namespace using kubectl.
// It supports both a snapshot as well as a continuous stream.
package pods

import (
	"context"
	"fmt"
	"sort"
	"strings"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/kubernetes"
)

// A PodStatus represents the status of a single Pod.
type PodStatus struct {
	Name           string // name of pod
	UID            types.UID
	Namespace      string
	Phase          corev1.PodPhase // current phase
	Containers     []ContainerStatus
	InitContainers []ContainerStatus
	Pod            corev1.Pod // copy of the raw pod
}

func (p *PodStatus) String() string {
	var buf strings.Builder
	add := func(k, v string) {
		if v != "" {
			fmt.Fprintf(&buf, ", %s: %q", k, v)
		}
	}
	fmt.Fprintf(&buf, "{Name: %q", p.Name)
	add("UID", string(p.UID))
	add("Namespace", p.Namespace)
	add("Phase", string(p.Phase))
	if len(p.Containers) > 0 {
		fmt.Fprintf(&buf, ", Containers: {%s", p.Containers[0].String())
		for _, c := range p.Containers[1:] {
			fmt.Fprintf(&buf, ", %s", c.String())
		}
		fmt.Fprintf(&buf, "}")
	}
	if len(p.InitContainers) > 0 {
		fmt.Fprintf(&buf, ", InitContainers: {%s", p.InitContainers[0].String())
		for _, c := range p.InitContainers[1:] {
			fmt.Fprintf(&buf, ", %s", c.String())
		}
		fmt.Fprintf(&buf, "}")
	}
	fmt.Fprintf(&buf, "}")
	return buf.String()
}

func (p *PodStatus) Equal(q *PodStatus) bool {
	if p.UID != q.UID ||
		p.Name != q.Name ||
		p.Namespace != q.Namespace ||
		p.Phase != q.Phase ||
		len(p.Containers) != len(q.Containers) ||
		len(p.InitContainers) != len(q.InitContainers) {
		return false
	}

	for i, c := range p.Containers {
		if !c.Equal(&q.Containers[i]) {
			return false
		}
	}

	for i, c := range p.InitContainers {
		if !c.Equal(&q.InitContainers[i]) {
			return false
		}
	}
	return true
}

// Values for Phase.  These constants are copied from k8s.io/api/core/v1 as a
// convenience.
const (
	PodPending   = corev1.PodPending   // Accepted but not all containers have started.
	PodRunning   = corev1.PodRunning   // All containers have started and at least one is running.
	PodSucceeded = corev1.PodSucceeded // All containers terminated without error.
	PodFailed    = corev1.PodFailed    // All containers terminated with at least one error.
)

// A ContainerStatus contains the status of a single container in the pod.
type ContainerStatus struct {
	Name    string
	Image   string // path of requested image
	Ready   bool   // true if ready
	Reason  string // if not empty, the reason it isn't ready
	Message string // human readable reason
	Raw     *corev1.ContainerStatus
}

func (c *ContainerStatus) String() string {
	var buf strings.Builder
	add := func(k, v string) {
		if v != "" {
			fmt.Fprintf(&buf, "%s: %q", k, v)
		}
	}
	fmt.Fprintf(&buf, "{Name: %q", c.Name)
	add("Image", c.Image)
	if c.Ready {
		add("Ready", "true")
	}
	add("Reason", c.Reason)
	add("Message", c.Message)
	fmt.Fprint(&buf, "}")
	return buf.String()
}

func (c *ContainerStatus) Equal(oc *ContainerStatus) bool {
	return !(c.Name != oc.Name ||
		c.Image != oc.Image ||
		c.Ready != oc.Ready ||
		c.Reason != oc.Reason ||
		c.Message != oc.Message)
}

// GetPodStatus returns the status of the pods found in the supplied namespace.
func GetPodStatus(ctx context.Context, client kubernetes.Interface, namespace string) ([]*PodStatus, error) {
	pods, err := client.CoreV1().Pods(namespace).List(ctx, metav1.ListOptions{})
	if err != nil {
		return nil, err
	}
	var statuses []*PodStatus
	for _, pod := range pods.Items {
		pod := pod // to keep the linter happy
		statuses = append(statuses, PodToStatus(&pod))
	}
	return statuses, nil
}

// WatchPodStatus returns a channel on which the status of the pods in the
// supplied namespace are written.
func WatchPodStatus(ctx context.Context, client kubernetes.Interface, namespace string) (chan *PodStatus, func(), error) {
	// Make sure we can even get the pods
	w, err := client.CoreV1().Pods(namespace).Watch(ctx, metav1.ListOptions{})
	if err != nil {
		return nil, nil, err
	}
	kch := w.ResultChan()
	ch := make(chan *PodStatus, 2)

	go func() {
		defer close(ch)
		// seen is used to drop duplicate updates
		seen := map[types.UID]*PodStatus{}
		for event := range kch {
			s := PodToStatus(event.Object.(*corev1.Pod))
			if os, ok := seen[s.UID]; ok {
				if s.Equal(os) {
					continue
				}
			}
			seen[s.UID] = s
			ch <- s
		}
	}()
	return ch, w.Stop, nil
}

// PodToStatus returns a pointer to a new PodStatus for pod.
func PodToStatus(pod *corev1.Pod) *PodStatus {
	s := PodStatus{
		Name:      pod.ObjectMeta.Name,
		Namespace: pod.ObjectMeta.Namespace,
		UID:       pod.ObjectMeta.UID,
		Phase:     pod.Status.Phase,
	}
	pod.DeepCopyInto(&s.Pod)
	pod = &s.Pod // Forget about the original copy

	for i, cs := range pod.Status.ContainerStatuses {
		c := ContainerStatus{
			Name:  cs.Name,
			Ready: cs.Ready,
			Image: cs.Image,
			Raw:   &pod.Status.ContainerStatuses[i],
		}
		if w := cs.State.Waiting; w != nil {
			c.Reason = w.Reason
			c.Message = w.Message
		}
		s.Containers = append(s.Containers, c)
	}
	sort.Slice(s.Containers, func(i, j int) bool {
		return s.Containers[i].Name < s.Containers[j].Name
	})
	for i, cs := range pod.Status.InitContainerStatuses {
		c := ContainerStatus{
			Name:  cs.Name,
			Ready: cs.Ready,
			Image: cs.Image,
			Raw:   &pod.Status.InitContainerStatuses[i],
		}
		if w := cs.State.Waiting; w != nil {
			c.Reason = w.Reason
			c.Message = w.Message
		}
		s.InitContainers = append(s.InitContainers, c)
	}
	sort.Slice(s.InitContainers, func(i, j int) bool {
		return s.InitContainers[i].Name < s.Containers[j].Name
	})
	return &s
}
