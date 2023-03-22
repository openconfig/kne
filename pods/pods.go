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
	"strings"
)

// A PodStatus represents the status of a single Pod.  Error is only set by
// WatchPodStatus when there was an error decoding the status.
type PodStatus struct {
	Name       string // name of pod
	Namespace  string
	Containers []ContainerStatus
	Phase      string // current phase
	Error      error  // If there was an error
}

type ContainerStatus struct {
	Name    string // name of pod
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

	m := map[string]any{}
	if err := json.Unmarshal(data, &m); err != nil {
		return nil, err
	}
	statuses := []PodStatus{}
	for _, i := range asList(follow(m, "items")) {
		if asString(follow(i, "kind")) == "Pod" {
			if s := podStatus(i); s.Name != "" {
				statuses = append(statuses, s)
			}
		}
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
		cmd.Stderr = os.Stderr
		stdout, err := cmd.StdoutPipe()
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
			m := map[string]any{}
			if err := d.Decode(&m); err != nil {
				if err != io.EOF {
					select {
					case <-quit:
					case ch <- PodStatus{Error: err}:
					}
				}
				return
			}
			if asString(follow(m, "kind")) == "Pod" {
				select {
				case <-quit:
					return
				case ch <- podStatus(m):
				}
			}
			select {
			case <-quit:
				return
			default:
			}
		}
	}()
	return ch, nil
}

func podStatus(m any) PodStatus {
	s := PodStatus{
		Name:      asString(follow(m, "metadata.name")),
		Namespace: asString(follow(m, "metadata.namespace")),
		Phase:     asString(follow(m, "status.phase")),
	}
	cstatus, ok := follow(m, "status.containerStatuses").([]any)
	if !ok {
		return s
	}
	for _, c := range cstatus {
		s.Containers = append(s.Containers, ContainerStatus{
			Name:    asString(follow(c, "name")),
			Ready:   asBool(follow(c, "ready")),
			Reason:  asString(follow(c, "state.waiting.reason")),
			Message: asString(follow(c, "state.waiting.message")),
		})
	}
	return s
}

// follow follows m down path.  If m is nil or path is empty then m is returned.
func follow(m any, path string) any {
	if m == nil || path == "" {
		return m
	}
	switch t := m.(type) {
	case map[string]any:
		x := strings.Index(path, ".")
		if x < 0 {
			return t[path]
		}
		node := path[:x]
		path = path[x+1:]
		if n, ok := t[node]; ok {
			return follow(n, path)
		}
		return nil
	case []any:
		switch len(t) {
		case 0:
		case 1:
			return follow(t[0], path)
		default:
			var a []any
			for _, i := range t {
				if r := follow(i, path); r != nil {
					a = append(a, r)
				}
			}
			return a
		}
	}
	return fmt.Sprintf("%s: can't index %T", path, m)
}

func asString(a any) string {
	switch s := a.(type) {
	case string:
		return s
	case map[string]any:
		return ""
	case []any:
		switch len(s) {
		case 0:
		case 1:
			return asString(s[0])
		}
	}
	return ""
}

func asBool(a any) bool {
	switch s := a.(type) {
	case bool:
		return s
	case map[string]any:
	case []any:
		switch len(s) {
		case 0:
		case 1:
			return asBool(s[0])
		}
	}
	return false
}

func asList(a any) []any {
	switch s := a.(type) {
	case []any:
		if len(s) == 0 {
			return nil
		}
		return s
	default:
		return []any{s}
	}
}
