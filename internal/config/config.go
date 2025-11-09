package config

import "time"

// ServerConfig holds configuration for server mode
type ServerConfig struct {
	Host string
	Port int
}

// HelperConfig holds configuration for helper mode
type HelperConfig struct {
	ControllerURL string
	StartupDelay  time.Duration
	Timeout       int
	PollInterval  time.Duration
}

// DockerConfig holds Docker client configuration
type DockerConfig struct {
	Host string
}
