package topo

import (
	"testing"
	"time"

	corev1 "k8s.io/api/core/v1"
)

func TestIsServiceReady(t *testing.T) {
	tests := []struct {
		desc string
		svc  *corev1.Service
		want bool
	}{
		{
			desc: "nil service",
			svc:  nil,
			want: false,
		},
		{
			desc: "unspecified service type defaults to ClusterIP",
			svc:  &corev1.Service{},
			want: true,
		},
		{
			desc: "ClusterIP service",
			svc: &corev1.Service{
				Spec: corev1.ServiceSpec{
					Type: corev1.ServiceTypeClusterIP,
				},
			},
			want: true,
		},
		{
			desc: "NodePort service",
			svc: &corev1.Service{
				Spec: corev1.ServiceSpec{
					Type: corev1.ServiceTypeNodePort,
				},
			},
			want: true,
		},
		{
			desc: "LoadBalancer service with no ingress",
			svc: &corev1.Service{
				Spec: corev1.ServiceSpec{
					Type: corev1.ServiceTypeLoadBalancer,
				},
			},
			want: false,
		},
		{
			desc: "LoadBalancer service with empty ingress",
			svc: &corev1.Service{
				Spec: corev1.ServiceSpec{
					Type: corev1.ServiceTypeLoadBalancer,
				},
				Status: corev1.ServiceStatus{
					LoadBalancer: corev1.LoadBalancerStatus{
						Ingress: []corev1.LoadBalancerIngress{{}},
					},
				},
			},
			want: false,
		},
		{
			desc: "LoadBalancer service with ingress IP",
			svc: &corev1.Service{
				Spec: corev1.ServiceSpec{
					Type: corev1.ServiceTypeLoadBalancer,
				},
				Status: corev1.ServiceStatus{
					LoadBalancer: corev1.LoadBalancerStatus{
						Ingress: []corev1.LoadBalancerIngress{{IP: "1.2.3.4"}},
					},
				},
			},
			want: true,
		},
		{
			desc: "LoadBalancer service with ingress hostname",
			svc: &corev1.Service{
				Spec: corev1.ServiceSpec{
					Type: corev1.ServiceTypeLoadBalancer,
				},
				Status: corev1.ServiceStatus{
					LoadBalancer: corev1.LoadBalancerStatus{
						Ingress: []corev1.LoadBalancerIngress{{Hostname: "lb.example.com"}},
					},
				},
			},
			want: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.desc, func(t *testing.T) {
			got := isServiceReady(tt.svc)
			if got != tt.want {
				t.Errorf("isServiceReady() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestCalculateServiceTimeout(t *testing.T) {
	tests := []struct {
		desc    string
		timeout time.Duration
		elapsed time.Duration
		want    time.Duration
	}{
		{
			desc:    "timeout 0 returns defaultServiceTimeout",
			timeout: 0,
			elapsed: 0,
			want:    2 * time.Minute,
		},
		{
			desc:    "timeout 0 with elapsed returns defaultServiceTimeout",
			timeout: 0,
			elapsed: 10 * time.Second,
			want:    2 * time.Minute,
		},
		{
			desc:    "timeout > minServiceTimeout with plenty of remaining time",
			timeout: 5 * time.Minute,
			elapsed: 1 * time.Minute,
			want:    4 * time.Minute,
		},
		{
			desc:    "timeout > minServiceTimeout but elapsed near timeout uses minServiceTimeout floor",
			timeout: 5 * time.Minute,
			elapsed: 4*time.Minute + 50*time.Second,
			want:    30 * time.Second,
		},
		{
			desc:    "timeout > minServiceTimeout and elapsed >= timeout uses minServiceTimeout floor",
			timeout: 5 * time.Minute,
			elapsed: 5*time.Minute + 10*time.Second,
			want:    30 * time.Second,
		},
		{
			desc:    "timeout < minServiceTimeout uses timeout as floor",
			timeout: time.Second,
			elapsed: 100 * time.Millisecond,
			want:    time.Second,
		},
		{
			desc:    "timeout < minServiceTimeout and elapsed >= timeout uses timeout as floor",
			timeout: time.Second,
			elapsed: 2 * time.Second,
			want:    time.Second,
		},
	}

	for _, tt := range tests {
		t.Run(tt.desc, func(t *testing.T) {
			got := calculateServiceTimeout(tt.timeout, tt.elapsed)
			if got != tt.want {
				t.Errorf("calculateServiceTimeout(%v, %v) = %v, want %v", tt.timeout, tt.elapsed, got, tt.want)
			}
		})
	}
}
