// This program is used to manually test the pods package against a running
// kubernetes cluster.
package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/openconfig/kne/pods"

	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/tools/clientcmd"
	"k8s.io/client-go/util/homedir"
)

func usage() {
	fmt.Fprintf(os.Stderr, "Usage: testcmd {get|watch} [namespace]\n")
	os.Exit(1)
}

func printStatus(s *pods.PodStatus) {
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
		fmt.Printf("\t\tImage: %s\n", c.Image)
	}
}

func main() {
	var kubeconfig, namespace *string
	if home := homedir.HomeDir(); home != "" {
		kubeconfig = flag.String("kubeconfig", filepath.Join(home, ".kube", "config"), "(optional) absolute path to the kubeconfig file")
	} else {
		kubeconfig = flag.String("kubeconfig", "", "absolute path to the kubeconfig file")
	}
	namespace = flag.String("namespace", "", "namespace to query")
	watch := flag.Bool("watch", false, "continue to watch")
	flag.Parse()

	// use the current context in kubeconfig
	config, err := clientcmd.BuildConfigFromFlags("", *kubeconfig)
	if err != nil {
		panic(err.Error())
	}

	// create the clientset
	clientset, err := kubernetes.NewForConfig(config)
	if err != nil {
		panic(err.Error())
	}

	if !*watch {
		statuses, err := pods.GetPodStatus(context.Background(), clientset, *namespace)
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
		for _, status := range statuses {
			printStatus(status)
		}
		return
	}
	ch, _, err := pods.WatchPodStatus(context.Background(), clientset, *namespace)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	for status := range ch {
		printStatus(status)
	}
}
