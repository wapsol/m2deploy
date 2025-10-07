package config

import (
	"testing"
)

func TestGetLocalImageName(t *testing.T) {
	tests := []struct {
		name      string
		config    *Config
		component string
		want      string
	}{
		{
			name: "backend with latest tag",
			config: &Config{
				LocalImageTag: "latest",
			},
			component: "backend",
			want:      "magnetiq/backend:latest",
		},
		{
			name: "frontend with version tag",
			config: &Config{
				LocalImageTag: "v1.2.3",
			},
			component: "frontend",
			want:      "magnetiq/frontend:v1.2.3",
		},
		{
			name: "backend with commit sha",
			config: &Config{
				LocalImageTag: "abc123",
			},
			component: "backend",
			want:      "magnetiq/backend:abc123",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.config.GetLocalImageName(tt.component); got != tt.want {
				t.Errorf("Config.GetLocalImageName() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestNewLogger(t *testing.T) {
	tests := []struct {
		name    string
		verbose bool
	}{
		{
			name:    "verbose logger",
			verbose: true,
		},
		{
			name:    "non-verbose logger",
			verbose: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			logger := NewLogger(tt.verbose)
			if logger == nil {
				t.Error("NewLogger() returned nil")
			}
			if logger.Verbose != tt.verbose {
				t.Errorf("NewLogger().Verbose = %v, want %v", logger.Verbose, tt.verbose)
			}
		})
	}
}
