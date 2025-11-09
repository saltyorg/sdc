package docker

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestParseLabels(t *testing.T) {
	tests := []struct {
		name     string
		labels   map[string]string
		expected *ContainerLabels
	}{
		{
			name: "fully configured container",
			labels: map[string]string{
				"com.github.saltbox.saltbox_managed":           "true",
				"com.github.saltbox.depends_on":                "traefik,redis",
				"com.github.saltbox.depends_on.delay":          "10",
				"com.github.saltbox.depends_on.healthchecks":   "true",
				"com.github.saltbox.saltbox_controller":        "true",
			},
			expected: &ContainerLabels{
				Managed:               true,
				DependsOn:             []string{"traefik", "redis"},
				DependsOnDelay:        10,
				DependsOnHealthchecks: true,
				ControllerEnabled:     true,
			},
		},
		{
			name: "minimal managed container",
			labels: map[string]string{
				"com.github.saltbox.saltbox_managed": "true",
			},
			expected: &ContainerLabels{
				Managed:               true,
				DependsOn:             []string{},
				DependsOnDelay:        0,
				DependsOnHealthchecks: false,
				ControllerEnabled:     true,
			},
		},
		{
			name: "controller disabled",
			labels: map[string]string{
				"com.github.saltbox.saltbox_managed":    "true",
				"com.github.saltbox.saltbox_controller": "false",
			},
			expected: &ContainerLabels{
				Managed:               true,
				DependsOn:             []string{},
				DependsOnDelay:        0,
				DependsOnHealthchecks: false,
				ControllerEnabled:     false,
			},
		},
		{
			name: "unmanaged container",
			labels: map[string]string{
				"com.github.saltbox.saltbox_managed": "false",
			},
			expected: &ContainerLabels{
				Managed:               false,
				DependsOn:             []string{},
				DependsOnDelay:        0,
				DependsOnHealthchecks: false,
				ControllerEnabled:     true,
			},
		},
		{
			name: "dependencies with whitespace",
			labels: map[string]string{
				"com.github.saltbox.saltbox_managed": "true",
				"com.github.saltbox.depends_on":      " traefik , redis , postgres ",
			},
			expected: &ContainerLabels{
				Managed:               true,
				DependsOn:             []string{"traefik", "redis", "postgres"},
				DependsOnDelay:        0,
				DependsOnHealthchecks: false,
				ControllerEnabled:     true,
			},
		},
		{
			name:   "empty labels",
			labels: map[string]string{},
			expected: &ContainerLabels{
				Managed:               false,
				DependsOn:             []string{},
				DependsOnDelay:        0,
				DependsOnHealthchecks: false,
				ControllerEnabled:     true,
			},
		},
		{
			name: "invalid delay ignored",
			labels: map[string]string{
				"com.github.saltbox.saltbox_managed":  "true",
				"com.github.saltbox.depends_on.delay": "invalid",
			},
			expected: &ContainerLabels{
				Managed:               true,
				DependsOn:             []string{},
				DependsOnDelay:        0,
				DependsOnHealthchecks: false,
				ControllerEnabled:     true,
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := ParseLabels(tt.labels)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestContainerLabels_IsManaged(t *testing.T) {
	tests := []struct {
		name     string
		labels   *ContainerLabels
		expected bool
	}{
		{
			name: "managed and enabled",
			labels: &ContainerLabels{
				Managed:           true,
				ControllerEnabled: true,
			},
			expected: true,
		},
		{
			name: "managed but disabled",
			labels: &ContainerLabels{
				Managed:           true,
				ControllerEnabled: false,
			},
			expected: false,
		},
		{
			name: "not managed",
			labels: &ContainerLabels{
				Managed:           false,
				ControllerEnabled: true,
			},
			expected: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := tt.labels.IsManaged()
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestContainerLabels_HasDependencies(t *testing.T) {
	tests := []struct {
		name     string
		labels   *ContainerLabels
		expected bool
	}{
		{
			name: "has dependencies",
			labels: &ContainerLabels{
				DependsOn: []string{"traefik"},
			},
			expected: true,
		},
		{
			name: "no dependencies",
			labels: &ContainerLabels{
				DependsOn: []string{},
			},
			expected: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := tt.labels.HasDependencies()
			assert.Equal(t, tt.expected, result)
		})
	}
}
