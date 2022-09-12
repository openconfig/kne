// Copyright 2021 Google LLC
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//	http://www.apache.org/licenses/LICENSE-2.0
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

	metallbv1 "go.universe.tf/metallb/api/v1beta1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/watch"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/kubernetes/scheme"
	"k8s.io/client-go/rest"
	"sigs.k8s.io/controller-runtime/pkg/client/apiutil"
)

// IPAddressPoolInterface provides access to the AddressPool CRD.
type IPAddressPoolInterface interface {
	List(ctx context.Context, opts metav1.ListOptions) (*metallbv1.AddressPoolList, error)
	Get(ctx context.Context, name string, opts metav1.GetOptions) (*metallbv1.AddressPool, error)
	Create(ctx context.Context, pool *metallbv1.IPAddressPool, opts metav1.CreateOptions) (*metallbv1.IPAddressPool, error)
	Delete(ctx context.Context, name string, opts metav1.DeleteOptions) error
	Watch(ctx context.Context, opts metav1.ListOptions) (watch.Interface, error)
	Unstructured(ctx context.Context, name string, opts metav1.GetOptions, subresources ...string) (*unstructured.Unstructured, error)
	Update(ctx context.Context, obj *unstructured.Unstructured, opts metav1.UpdateOptions) (*metallbv1.IPAddressPool, error)
}

// Interface is the clientset interface for Metallb.
type Interface interface {
	IPAddressPool(namespace string) IPAddressPoolInterface
}

// Clientset is a client for the metallb crds.
type Clientset struct {
	dInterface dynamic.NamespaceableResourceInterface
}

var gvrIPAddressPool = schema.GroupVersionResource{
	Group:    groupVersion.Group,
	Version:  groupVersion.Version,
	Resource: "ipaddresspools",
}

func IPAddressPoolGVR() schema.GroupVersionResource {
	return gvrIPAddressPool
}

var (
	groupVersion = metallbv1.GroupVersion
	Scheme       = runtime.NewScheme()
)

func init() {
	metallbv1.AddToScheme(Scheme)

	metav1.AddToGroupVersion(Scheme, groupVersion)
	metav1.AddMetaToScheme(Scheme)
}

func GV() *schema.GroupVersion {
	return &groupVersion
}

// NewAddressPoolForConfig returns a new Clientset based on c.
func NewAddressPoolForConfig(c *rest.Config) (*Clientset, error) {
	config := *c
	config.ContentConfig.GroupVersion = &groupVersion
	config.APIPath = "/apis"
	config.NegotiatedSerializer = scheme.Codecs.WithoutConversion()
	config.UserAgent = rest.DefaultKubernetesUserAgent()
	dClient, err := dynamic.NewForConfig(c)
	if err != nil {
		return nil, err
	}
	dInterface := dClient.Resource(gvrIPAddressPool)
	return &Clientset{dInterface: dInterface}, nil
}

// SetDynamicClient is only exposed for integration testing.
func (c *Clientset) SetDynamicClient(d dynamic.NamespaceableResourceInterface) {
	c.dInterface = d
}

func (c *Clientset) IPAddressPool(namespace string) IPAddressPoolInterface {
	return &addressPoolClient{
		dInterface: c.dInterface,
		ns:         namespace,
	}
}

type addressPoolClient struct {
	dInterface dynamic.NamespaceableResourceInterface
	ns         string
}

func (a *addressPoolClient) List(ctx context.Context, opts metav1.ListOptions) (*metallbv1.AddressPoolList, error) {
	u, err := a.dInterface.Namespace(a.ns).List(ctx, opts)
	if err != nil {
		return nil, err
	}
	result := metallbv1.AddressPoolList{}
	if err = runtime.DefaultUnstructuredConverter.FromUnstructured(u.UnstructuredContent(), &result); err != nil {
		return nil, fmt.Errorf("failed to type assert return to AddressPoolList: %w", err)
	}
	return &result, nil
}

func (a *addressPoolClient) Get(ctx context.Context, name string, opts metav1.GetOptions) (*metallbv1.AddressPool, error) {
	u, err := a.dInterface.Namespace(a.ns).Get(ctx, name, opts)
	if err != nil {
		return nil, err
	}
	result := metallbv1.AddressPool{}
	if err = runtime.DefaultUnstructuredConverter.FromUnstructured(u.UnstructuredContent(), &result); err != nil {
		return nil, fmt.Errorf("failed to type assert return to AddressPool: %w", err)
	}
	return &result, nil
}

func (a *addressPoolClient) Create(ctx context.Context, pool *metallbv1.IPAddressPool, opts metav1.CreateOptions) (*metallbv1.IPAddressPool, error) {
	gvk, err := apiutil.GVKForObject(pool, Scheme)
	if err != nil {
		return nil, fmt.Errorf("failed to get gvk for AddressPool: %w", err)
	}
	pool.TypeMeta = metav1.TypeMeta{
		Kind:       gvk.Kind,
		APIVersion: gvk.GroupVersion().String(),
	}
	obj, err := runtime.DefaultUnstructuredConverter.ToUnstructured(pool)
	if err != nil {
		return nil, fmt.Errorf("failed to convert IPAddressPool to unstructured: %w", err)
	}
	u, err := a.dInterface.Namespace(a.ns).Create(ctx, &unstructured.Unstructured{Object: obj}, opts)
	if err != nil {
		return nil, err
	}
	result := metallbv1.IPAddressPool{}
	if err = runtime.DefaultUnstructuredConverter.FromUnstructured(u.UnstructuredContent(), &result); err != nil {
		return nil, fmt.Errorf("failed to type assert return to AddressPool: %w", err)
	}
	return &result, nil
}

func (a *addressPoolClient) Watch(ctx context.Context, opts metav1.ListOptions) (watch.Interface, error) {
	opts.Watch = true
	return a.dInterface.Namespace(a.ns).Watch(ctx, opts)
}

func (a *addressPoolClient) Delete(ctx context.Context, name string, opts metav1.DeleteOptions) error {
	return a.dInterface.Namespace(a.ns).Delete(ctx, name, opts)
}

func (a *addressPoolClient) Update(ctx context.Context, obj *unstructured.Unstructured, opts metav1.UpdateOptions) (*metallbv1.IPAddressPool, error) {
	obj, err := a.dInterface.Namespace(a.ns).UpdateStatus(ctx, obj, metav1.UpdateOptions{})
	if err != nil {
		return nil, err
	}
	result := metallbv1.IPAddressPool{}
	if err = runtime.DefaultUnstructuredConverter.FromUnstructured(obj.UnstructuredContent(), &result); err != nil {
		return nil, fmt.Errorf("failed to type assert return to AddressPool: %w", err)
	}
	return &result, nil
}

func (a *addressPoolClient) Unstructured(ctx context.Context, name string, opts metav1.GetOptions, subresources ...string) (*unstructured.Unstructured, error) {
	return a.dInterface.Namespace(a.ns).Get(ctx, name, opts, subresources...)
}
