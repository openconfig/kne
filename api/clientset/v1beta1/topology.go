// Copyright 2021 Google LLC
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//      http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package v1beta1

import (
	"context"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/watch"
	"k8s.io/client-go/kubernetes/scheme"
	"k8s.io/client-go/rest"

	topologyv1 "github.com/hfam/kne/api/types/v1beta1"
)

// TopologyInterface provides access to the Topology CRD.
type TopologyInterface interface {
	List(ctx context.Context, opts metav1.ListOptions) (*topologyv1.TopologyList, error)
	Get(ctx context.Context, name string, options metav1.GetOptions) (*topologyv1.Topology, error)
	Create(ctx context.Context, topology *topologyv1.Topology) (*topologyv1.Topology, error)
	Watch(ctx context.Context, opts metav1.ListOptions) (watch.Interface, error)
}

// Interface is the clientset interface for topology.
type Interface interface {
	Topology(namespace string) TopologyInterface
}

// Clientset is a client for the topology crds.
type Clientset struct {
	restClient rest.Interface
}

// NewForConfig returns a new Clientset based on c.
func NewForConfig(c *rest.Config) (*Clientset, error) {
	config := *c
	config.ContentConfig.GroupVersion = &schema.GroupVersion{Group: topologyv1.GroupName, Version: topologyv1.GroupVersion}
	config.APIPath = "/apis"
	config.NegotiatedSerializer = scheme.Codecs.WithoutConversion()
	config.UserAgent = rest.DefaultKubernetesUserAgent()

	client, err := rest.RESTClientFor(&config)
	if err != nil {
		return nil, err
	}

	return &Clientset{restClient: client}, nil
}

func (c *Clientset) Topology(namespace string) TopologyInterface {
	return &topologyClient{
		restClient: c.restClient,
		ns:         namespace,
	}
}

type topologyClient struct {
	restClient rest.Interface
	ns         string
}

func (t *topologyClient) List(ctx context.Context, opts metav1.ListOptions) (*topologyv1.TopologyList, error) {
	result := topologyv1.TopologyList{}
	err := t.restClient.
		Get().
		Namespace(t.ns).
		Resource("topologies").
		VersionedParams(&opts, scheme.ParameterCodec).
		Do(ctx).
		Into(&result)

	return &result, err
}

func (t *topologyClient) Get(ctx context.Context, name string, opts metav1.GetOptions) (*topologyv1.Topology, error) {
	result := topologyv1.Topology{}
	err := t.restClient.
		Get().
		Namespace(t.ns).
		Resource("topologies").
		Name(name).
		VersionedParams(&opts, scheme.ParameterCodec).
		Do(ctx).
		Into(&result)

	return &result, err
}

func (t *topologyClient) Create(ctx context.Context, topology *topologyv1.Topology) (*topologyv1.Topology, error) {
	result := topologyv1.Topology{}
	err := t.restClient.
		Post().
		Namespace(t.ns).
		Resource("topologies").
		Body(topology).
		Do(ctx).
		Into(&result)

	return &result, err
}

func (t *topologyClient) Watch(ctx context.Context, opts metav1.ListOptions) (watch.Interface, error) {
	opts.Watch = true
	return t.restClient.
		Get().
		Namespace(t.ns).
		Resource("topology").
		VersionedParams(&opts, scheme.ParameterCodec).
		Watch(ctx)
}

func init() {
	topologyv1.AddToScheme(scheme.Scheme)
}
