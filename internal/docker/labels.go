package docker

import (
	"strconv"
	"strings"
)

// ContainerLabels represents parsed Saltbox labels
type ContainerLabels struct {
	Managed               bool
	DependsOn             []string
	DependsOnDelay        int
	DependsOnHealthchecks bool
	ControllerEnabled     bool
}

// ParseLabels extracts and parses Saltbox-specific labels from a container
func ParseLabels(labels map[string]string) *ContainerLabels {
	parsed := &ContainerLabels{
		Managed:               false,
		DependsOn:             []string{},
		DependsOnDelay:        0,
		DependsOnHealthchecks: false,
		ControllerEnabled:     true, // Default to enabled
	}

	// Check if container is managed
	if managed, ok := labels["com.github.saltbox.saltbox_managed"]; ok {
		parsed.Managed = strings.ToLower(managed) == "true"
	}

	// Check if controller is enabled (opt-out mechanism)
	if controller, ok := labels["com.github.saltbox.saltbox_controller"]; ok {
		parsed.ControllerEnabled = strings.ToLower(controller) != "false"
	}

	// Parse dependencies
	if dependsOn, ok := labels["com.github.saltbox.depends_on"]; ok && dependsOn != "" {
		// Split by comma and trim whitespace
		deps := strings.SplitSeq(dependsOn, ",")
		for dep := range deps {
			trimmed := strings.TrimSpace(dep)
			if trimmed != "" {
				parsed.DependsOn = append(parsed.DependsOn, trimmed)
			}
		}
	}

	// Parse startup delay
	if delay, ok := labels["com.github.saltbox.depends_on.delay"]; ok {
		if delayInt, err := strconv.Atoi(delay); err == nil && delayInt > 0 {
			parsed.DependsOnDelay = delayInt
		}
	}

	// Parse healthcheck flag
	if healthchecks, ok := labels["com.github.saltbox.depends_on.healthchecks"]; ok {
		parsed.DependsOnHealthchecks = strings.ToLower(healthchecks) == "true"
	}

	return parsed
}

// IsManaged returns true if the container should be managed by the controller
func (l *ContainerLabels) IsManaged() bool {
	return l.Managed && l.ControllerEnabled
}

// HasDependencies returns true if the container has any dependencies
func (l *ContainerLabels) HasDependencies() bool {
	return len(l.DependsOn) > 0
}

// GetDependencies returns the list of container names this container depends on
func (l *ContainerLabels) GetDependencies() []string {
	return l.DependsOn
}

// GetStartupDelay returns the startup delay in seconds
func (l *ContainerLabels) GetStartupDelay() int {
	return l.DependsOnDelay
}

// ShouldWaitForHealthcheck returns true if we should wait for health checks
func (l *ContainerLabels) ShouldWaitForHealthcheck() bool {
	return l.DependsOnHealthchecks
}
