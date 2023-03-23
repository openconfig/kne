// Package pods provides pod status for a namespace using kubectl.
// It supports both a snapshot as well as a continuous stream.
package pods

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"

	corev1 "k8s.io/api/core/v1"
)

// A PodStatus represents the status of a single Pod.  Error is only set by
// WatchPodStatus when there was an error decoding the status.
type PodStatus struct {
	Name       string // name of pod
	Namespace  string
	Containers []ContainerStatus
	Phase      corev1.PodPhase // current phase
	Error      error           // If there was an error
}

// These constants are copied from k8s.io/api/core/v1 as a convenience.
const (
	PodPending   = corev1.PodPending   // Accepted but not all containers have started.
	PodRunning   = corev1.PodRunning   // All containers have started and at least one is running.
	PodSucceeded = corev1.PodSucceeded // All containers terminated without error.
	PodFailed    = corev1.PodFailed    // All containers terminated with at least one error.
)

// A ContainerStatus contains the status of a single container in the pod.
type ContainerStatus struct {
	Name    string // name of container
	Image   string // path of requested image
	Ready   bool   // true if ready
	Reason  string // if not empty, the reason it isn't ready
	Message string // human readable reason
}

func (s PodStatus) String() string {
	if s.Error != nil {
		return "Error: " + s.Error.Error()
	}
	return fmt.Sprintf("%s: Phase=%s %v", s.Name, s.Phase, s.Containers)
}

var testData []byte

// GetPodStatus returns the status of the pods found in the supplied namespace.
func GetPodStatus(namespace string) ([]PodStatus, error) {
	var data []byte
	if testData == nil {
		var stdout bytes.Buffer
		args := []string{"get", "pods", "-o", "json"}
		if namespace != "" {
			args = append(args, "-n", namespace)
		} else {
			args = append(args, "-A")
		}
		cmd := exec.Command("kubectl", args...)
		cmd.Stdout = &stdout
		cmd.Stderr = os.Stderr
		if err := cmd.Run(); err != nil {
			return nil, err
		}
		data = stdout.Bytes()
	} else {
		data = testData
	}

	var pods corev1.PodList
	var statuses []PodStatus
	if err := json.Unmarshal(data, &pods); err != nil {
		return nil, err
	}
	for _, pod := range pods.Items {
		statuses = append(statuses, podStatus(pod))
	}
	return statuses, nil
}

var testReader io.Reader

// WatchPodStatus returns a channel on which the status of the pods in the
// supplied namespace are written.  If quit is closed then WatchPodSatus's go
// routine will exit.  Passing nil as quit will cause the go routine to only
// exit if there is an error watching the pods.
func WatchPodStatus(namespace string, quit chan struct{}) (chan PodStatus, error) {
	var r io.Reader
	if testReader == nil {
		args := []string{"get", "pods", "-o", "json", "--watch"}
		if namespace != "" {
			args = append(args, "-n", namespace)
		} else {
			args = append(args, "-A")
		}
		fmt.Printf("args: %q\n", args)
		cmd := exec.Command("kubectl", args...)
		stdout, err := cmd.StdoutPipe()
		cmd.Stderr = os.Stderr
		if err != nil {
			return nil, err
		}
		if err := cmd.Start(); err != nil {
			return nil, err
		}
		go cmd.Wait()
		r = stdout
	} else {
		r = testReader
	}
	ch := make(chan PodStatus, 2)
	go func() {
		defer close(ch)
		d := json.NewDecoder(r)
		for {
			var pod corev1.Pod
			if err := d.Decode(&pod); err != nil {
				if err != io.EOF {
					select {
					case <-quit:
					case ch <- PodStatus{Error: err}:
					}
				}
				return
			}
			select {
			case <-quit:
				return
			case ch <- podStatus(pod):
			}
		}
	}()
	return ch, nil
}

func podStatus(pod corev1.Pod) PodStatus {
	s := PodStatus{
		Name:      pod.ObjectMeta.Name,
		Namespace: pod.ObjectMeta.Namespace,
		Phase:     pod.Status.Phase,
	}
	for _, cs := range pod.Status.ContainerStatuses {
		c := ContainerStatus{
			Name:  cs.Name,
			Ready: cs.Ready,
			Image: cs.Image,
		}
		if w := cs.State.Waiting; w != nil {
			c.Reason = w.Reason
			c.Message = w.Message
		}
		s.Containers = append(s.Containers, c)
	}
	return s
}
