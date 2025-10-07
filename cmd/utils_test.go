package cmd

import (
	"testing"
)

func TestGetComponents(t *testing.T) {
	tests := []struct {
		name      string
		component string
		want      []string
	}{
		{
			name:      "backend component",
			component: ComponentBackend,
			want:      []string{ComponentBackend},
		},
		{
			name:      "frontend component",
			component: ComponentFrontend,
			want:      []string{ComponentFrontend},
		},
		{
			name:      "both components",
			component: ComponentBoth,
			want:      []string{ComponentBackend, ComponentFrontend},
		},
		{
			name:      "invalid component",
			component: "invalid",
			want:      []string{},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := getComponents(tt.component)
			if len(got) != len(tt.want) {
				t.Errorf("getComponents() length = %v, want %v", len(got), len(tt.want))
				return
			}
			for i := range got {
				if got[i] != tt.want[i] {
					t.Errorf("getComponents()[%d] = %v, want %v", i, got[i], tt.want[i])
				}
			}
		})
	}
}

func TestValidateComponent(t *testing.T) {
	tests := []struct {
		name      string
		component string
		wantErr   bool
	}{
		{
			name:      "valid backend",
			component: ComponentBackend,
			wantErr:   false,
		},
		{
			name:      "valid frontend",
			component: ComponentFrontend,
			wantErr:   false,
		},
		{
			name:      "valid both",
			component: ComponentBoth,
			wantErr:   false,
		},
		{
			name:      "invalid component",
			component: "invalid",
			wantErr:   true,
		},
		{
			name:      "empty component",
			component: "",
			wantErr:   true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := validateComponent(tt.component)
			if (err != nil) != tt.wantErr {
				t.Errorf("validateComponent() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func TestGetDeploymentName(t *testing.T) {
	tests := []struct {
		name      string
		component string
		want      string
	}{
		{
			name:      "backend deployment",
			component: ComponentBackend,
			want:      "magnetiq-backend",
		},
		{
			name:      "frontend deployment",
			component: ComponentFrontend,
			want:      "magnetiq-frontend",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := getDeploymentName(tt.component); got != tt.want {
				t.Errorf("getDeploymentName() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestGetTestContainerName(t *testing.T) {
	tests := []struct {
		name      string
		component string
		want      string
	}{
		{
			name:      "backend test container",
			component: ComponentBackend,
			want:      "m2deploy-test-backend",
		},
		{
			name:      "frontend test container",
			component: ComponentFrontend,
			want:      "m2deploy-test-frontend",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := getTestContainerName(tt.component); got != tt.want {
				t.Errorf("getTestContainerName() = %v, want %v", got, tt.want)
			}
		})
	}
}
