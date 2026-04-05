package k8s

import (
	"testing"
)

func TestIsCrossplaneGroup(t *testing.T) {
	tests := []struct {
		group string
		want  bool
	}{
		{"apiextensions.crossplane.io", true},
		{"pkg.crossplane.io", true},
		{"s3.aws.upbound.io", true},
		{"rds.aws.upbound.io", true},
		{"upbound.io", true},
		{"compute.gcp.upbound.io", true},
		{"apps", false},
		{"networking.k8s.io", false},
		{"argoproj.io", false},
		{"", false},
	}

	for _, tt := range tests {
		got := IsCrossplaneGroup(tt.group)
		if got != tt.want {
			t.Errorf("IsCrossplaneGroup(%q) = %v, want %v", tt.group, got, tt.want)
		}
	}
}

func TestIsCoreKubernetesGroup(t *testing.T) {
	tests := []struct {
		group string
		want  bool
	}{
		{"", true},
		{"apps", true},
		{"batch", true},
		{"rbac.authorization.k8s.io", true},
		{"networking.k8s.io", true},
		{"apiextensions.k8s.io", true},
		{"storage.example.org", false},
		{"s3.aws.upbound.io", false},
	}

	for _, tt := range tests {
		got := isCoreKubernetesGroup(tt.group)
		if got != tt.want {
			t.Errorf("isCoreKubernetesGroup(%q) = %v, want %v", tt.group, got, tt.want)
		}
	}
}

func TestShouldWatch(t *testing.T) {
	extra := map[string]bool{
		"custom.example.org": true,
	}

	tests := []struct {
		group string
		want  bool
	}{
		{"apiextensions.crossplane.io", true},
		{"s3.aws.upbound.io", true},
		{"custom.example.org", true},
		{"apps", false},
		{"argoproj.io", false},
	}

	for _, tt := range tests {
		got := shouldWatch(tt.group, extra)
		if got != tt.want {
			t.Errorf("shouldWatch(%q) = %v, want %v", tt.group, got, tt.want)
		}
	}
}
