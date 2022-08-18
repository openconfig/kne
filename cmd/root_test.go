package cmd

import (
	"os"
	"testing"
)

func TestGetKubeCfg(t *testing.T) {
	tests := []struct {
		desc   string
		setVar func() func()
		want   string
	}{{
		desc: "KUBECONFIG Set",
		setVar: func() func() {
			orig := os.Getenv("KUBECONFIG")
			os.Setenv("KUBECONFIG", "/etc/kube/config")
			return func() {
				os.Setenv("KUBECONFIG", orig)
			}
		},
		want: "/etc/kube/config",
	}, {
		desc: "home dir",
		setVar: func() func() {
			orig := os.Getenv("HOME")
			os.Setenv("HOME", "/fakeuser")
			return func() {
				os.Setenv("HOME", orig)
			}
		},
		want: "/fakeuser/.kube/config",
	}, {
		desc: "unknown",
		setVar: func() func() {
			origH := os.Getenv("HOME")
			os.Setenv("HOME", "")
			origK := os.Getenv("KUBECONFIG")
			os.Setenv("KUBECONFIG", "")
			return func() {
				os.Setenv("HOME", origH)
				os.Setenv("KUBECONFIG", origK)
			}
		},
		want: "",
	}}
	for _, tt := range tests {
		t.Run(tt.desc, func(t *testing.T) {
			unset := tt.setVar()
			defer unset()
			got := defaultKubeCfg()
			if got != tt.want {
				t.Fatalf("getKubeCfg() failed: got %v, want %v", got, tt.want)
			}
		})
	}
}
