// This program is used to manually test the pods package against a running
// kubernetes cluster.
package main

import (
	"fmt"
	"os"

	"github.com/openconfig/kne/pods"
)

func usage() {
	fmt.Fprintf(os.Stderr, "Usage: testcmd {get|watch} [namespace]\n")
	os.Exit(1)
}

func printStatus(s pods.PodStatus) {
	if s.Error != nil {
		fmt.Printf("Error: %v\n", s.Error)
		return
	}
	fmt.Printf("%s:%s: %s\n", s.Namespace, s.Name, s.Phase)
	for _, c := range s.Containers {
		switch {
		case c.Ready:
			fmt.Printf("\t%s: ready\n", c.Name)
		case c.Message == "":
			fmt.Printf("\t%s: %s\n", c.Name, c.Reason)
		default:
			fmt.Printf("\t%s: %s: %s\n", c.Name, c.Reason, c.Message)
		}
	}
}

func main() {
	var namespace string
	switch len(os.Args) {
	case 2:
	case 3:
		namespace = os.Args[2]
	default:
		usage()
	}
	switch os.Args[1] {
	case "get":
		statuses, err := pods.GetPodStatus(namespace)
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
		for _, status := range statuses {
			printStatus(status)
		}
	case "watch":
		ch, err := pods.WatchPodStatus(namespace, nil)
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
		for status := range ch {
			printStatus(status)
		}
	default:
		usage()
	}
}
