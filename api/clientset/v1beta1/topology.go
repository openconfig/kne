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
	"fmt"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/watch"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/kubernetes/scheme"
	"k8s.io/client-go/rest"

	topologyv1 "github.com/google/kne/api/types/v1beta1"
)

// TopologyInterface provides access to the Topology CRD.
type TopologyInterface interface {
	List(ctx context.Context, opts metav1.ListOptions) (*topologyv1.TopologyList, error)
	Get(ctx context.Context, name string, options metav1.GetOptions) (*topologyv1.Topology, error)
	Create(ctx context.Context, topology *topologyv1.Topology) (*topologyv1.Topology, error)
	Delete(ctx context.Context, name string, opts metav1.DeleteOptions) error
	Watch(ctx context.Context, opts metav1.ListOptions) (watch.Interface, error)
	Unstructured(ctx context.Context, name string, opts metav1.GetOptions, subresources ...string) (*unstructured.Unstructured, error)
	Update(ctx context.Context, obj *unstructured.Unstructured, opts metav1.UpdateOptions) (*topologyv1.Topology, error)
}

// Interface is the clientset interface for topology.
type Interface interface {
	Topology(namespace string) TopologyInterface
}

// Clientset is a client for the topology crds.
type Clientset struct {
	dInterface dynamic.NamespaceableResourceInterface
	restClient rest.Interface
}

var gvr = schema.GroupVersionResource{
	Group:    "networkop.co.uk",
	Version:  "v1beta1",
	Resource: "topologies",
}

func GVR() schema.GroupVersionResource {
	return gvr
}

var (
	groupVersion = &schema.GroupVersion{
		Group:   topologyv1.GroupName,
		Version: topologyv1.GroupVersion,
	}
)

func GV() *schema.GroupVersion {
	return groupVersion
}

// NewForConfig returns a new Clientset based on c.
func NewForConfig(c *rest.Config) (*Clientset, error) {
	config := *c
	config.ContentConfig.GroupVersion = groupVersion
	config.APIPath = "/apis"
	config.NegotiatedSerializer = scheme.Codecs.WithoutConversion()
	config.UserAgent = rest.DefaultKubernetesUserAgent()
	dClient, err := dynamic.NewForConfig(c)
	if err != nil {
		return nil, err
	}
	dInterface := dClient.Resource(gvr)
	rClient, err := rest.RESTClientFor(&config)
	if err != nil {
		return nil, err
	}
	return &Clientset{
		dInterface: dInterface,
		restClient: rClient,
	}, nil
}

func (c *Clientset) SetDynamicClient(d dynamic.NamespaceableResourceInterface) {
	c.dInterface = d
}

func (c *Clientset) SetRestClient(r rest.Interface) {
	c.restClient = r
}

func (c *Clientset) Topology(namespace string) TopologyInterface {
	return &topologyClient{
		dInterface: c.dInterface,
		restClient: c.restClient,
		ns:         namespace,
	}
}

type topologyClient struct {
	dInterface dynamic.NamespaceableResourceInterface
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
		Resource("topologies").
		VersionedParams(&opts, scheme.ParameterCodec).
		Watch(ctx)
}

func (t *topologyClient) Delete(ctx context.Context, name string, opts metav1.DeleteOptions) error {
	return t.restClient.
		Delete().
		Namespace(t.ns).
		Resource("topologies").
		VersionedParams(&opts, scheme.ParameterCodec).
		Name(name).
		Do(ctx).
		Error()
}

func (t *topologyClient) Update(ctx context.Context, obj *unstructured.Unstructured, opts metav1.UpdateOptions) (*topologyv1.Topology, error) {
	result := topologyv1.Topology{}
	obj, err := t.dInterface.Namespace(t.ns).UpdateStatus(ctx, obj, metav1.UpdateOptions{})
	if err != nil {
		return nil, err
	}
	err = runtime.DefaultUnstructuredConverter.FromUnstructured(obj.UnstructuredContent(), &result)
	if err != nil {
		return nil, fmt.Errorf("failed to type assert return to topology")
	}
	return &result, nil
}

func (t *topologyClient) Unstructured(ctx context.Context, name string, opts metav1.GetOptions, subresources ...string) (*unstructured.Unstructured, error) {
	return t.dInterface.Namespace(t.ns).Get(ctx, name, opts, subresources...)
}

func init() {
	topologyv1.AddToScheme(scheme.Scheme)
}
