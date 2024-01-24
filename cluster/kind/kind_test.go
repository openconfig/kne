package kind

import (
	"testing"

	"github.com/openconfig/gnmi/errdiff"
)

func TestClusterIsKind(t *testing.T) {
	tests := []struct{
		desc string
		wantErr string
	}{{
	}}
	for _, tt := range tests {
		t.Run(tt.desc, func(t *testing.T) {
			if s := errdiff.Substring(nil, tt.wantErr); s != "" {
				t.Fatalf("unexpected error: %s", s)
			}
		})
	}
}

func TestClusterKindName(t *testing.T) {
	tests := []struct{
		desc string
		wantErr string
	}{{
	}}
	for _, tt := range tests {
		t.Run(tt.desc, func(t *testing.T) {
			if s := errdiff.Substring(nil, tt.wantErr); s != "" {
				t.Fatalf("unexpected error: %s", s)
			}
		})
	}
}

func TestClusterKindNodes(t *testing.T) {
	tests := []struct{
		desc string
		wantErr string
	}{{
	}}
	for _, tt := range tests {
		t.Run(tt.desc, func(t *testing.T) {
			if s := errdiff.Substring(nil, tt.wantErr); s != "" {
				t.Fatalf("unexpected error: %s", s)
			}
		})
	}
}

func TestSetupGARAccess(t *testing.T) {
	tests := []struct{
		desc string
		wantErr string
	}{{
	}}
	for _, tt := range tests {
		t.Run(tt.desc, func(t *testing.T) {
			if s := errdiff.Substring(nil, tt.wantErr); s != "" {
				t.Fatalf("unexpected error: %s", s)
			}
		})
	}
}

func TestRefreshGARAccess(t *testing.T) {
	tests := []struct{
		desc string
		wantErr string
	}{{
	}}
	for _, tt := range tests {
		t.Run(tt.desc, func(t *testing.T) {
			if s := errdiff.Substring(nil, tt.wantErr); s != "" {
				t.Fatalf("unexpected error: %s", s)
			}
		})
	}
}
