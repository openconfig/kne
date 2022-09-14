// Copyright 2022 Google LLC
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
	"k8s.io/apimachinery/pkg/watch"
	"k8s.io/client-go/dynamic"
	"sigs.k8s.io/controller-runtime/pkg/client/apiutil"
)

type l2AdvertisementClient struct {
	dInterface dynamic.NamespaceableResourceInterface
	ns         string
}

func (l *l2AdvertisementClient) List(ctx context.Context, opts metav1.ListOptions) (*metallbv1.L2AdvertisementList, error) {
	u, err := l.dInterface.Namespace(l.ns).List(ctx, opts)
	if err != nil {
		return nil, err
	}
	result := metallbv1.L2AdvertisementList{}
	if err = runtime.DefaultUnstructuredConverter.FromUnstructured(u.UnstructuredContent(), &result); err != nil {
		return nil, fmt.Errorf("failed to type assert return to L2AdvertisementList: %w", err)
	}
	return &result, nil
}

func (l *l2AdvertisementClient) Get(ctx context.Context, name string, opts metav1.GetOptions) (*metallbv1.L2Advertisement, error) {
	u, err := l.dInterface.Namespace(l.ns).Get(ctx, name, opts)
	if err != nil {
		return nil, err
	}
	result := metallbv1.L2Advertisement{}
	if err = runtime.DefaultUnstructuredConverter.FromUnstructured(u.UnstructuredContent(), &result); err != nil {
		return nil, fmt.Errorf("failed to type assert return to L2Advertisement: %w", err)
	}
	return &result, nil
}

func (l *l2AdvertisementClient) Create(ctx context.Context, pool *metallbv1.L2Advertisement, opts metav1.CreateOptions) (*metallbv1.L2Advertisement, error) {
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
		return nil, fmt.Errorf("failed to convert L2Advertisement to unstructured: %w", err)
	}
	u, err := l.dInterface.Namespace(l.ns).Create(ctx, &unstructured.Unstructured{Object: obj}, opts)
	if err != nil {
		return nil, err
	}
	result := metallbv1.L2Advertisement{}
	if err = runtime.DefaultUnstructuredConverter.FromUnstructured(u.UnstructuredContent(), &result); err != nil {
		return nil, fmt.Errorf("failed to type assert return to AddressPool: %w", err)
	}
	return &result, nil
}

func (l *l2AdvertisementClient) Watch(ctx context.Context, opts metav1.ListOptions) (watch.Interface, error) {
	opts.Watch = true
	return l.dInterface.Namespace(l.ns).Watch(ctx, opts)
}

func (l *l2AdvertisementClient) Delete(ctx context.Context, name string, opts metav1.DeleteOptions) error {
	return l.dInterface.Namespace(l.ns).Delete(ctx, name, opts)
}

func (l *l2AdvertisementClient) Update(ctx context.Context, obj *unstructured.Unstructured, opts metav1.UpdateOptions) (*metallbv1.L2Advertisement, error) {
	obj, err := l.dInterface.Namespace(l.ns).UpdateStatus(ctx, obj, metav1.UpdateOptions{})
	if err != nil {
		return nil, err
	}
	result := metallbv1.L2Advertisement{}
	if err = runtime.DefaultUnstructuredConverter.FromUnstructured(obj.UnstructuredContent(), &result); err != nil {
		return nil, fmt.Errorf("failed to type assert return to AddressPool: %w", err)
	}
	return &result, nil
}

func (l *l2AdvertisementClient) Unstructured(ctx context.Context, name string, opts metav1.GetOptions, subresources ...string) (*unstructured.Unstructured, error) {
	return l.dInterface.Namespace(l.ns).Get(ctx, name, opts, subresources...)
}
