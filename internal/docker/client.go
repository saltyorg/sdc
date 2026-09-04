package docker

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/moby/moby/client"
	"github.com/saltyorg/sdc/internal/lifecycle"
	"github.com/saltyorg/sdc/pkg/logger"
)

type Client struct {
	cli    *client.Client
	logger *logger.Logger
}

type connectionConfig struct {
	maxRetries     int
	initialBackoff time.Duration
	maxBackoff     time.Duration
	timeout        time.Duration
}

func defaultConnectionConfig() connectionConfig {
	return connectionConfig{
		maxRetries:     5,
		initialBackoff: time.Second,
		maxBackoff:     30 * time.Second,
		timeout:        10 * time.Second,
	}
}

// New connects to Saltbox's local rootful Docker daemon.
func New(ctx context.Context, logger *logger.Logger) (*Client, error) {
	return newWithConfig(ctx, logger, defaultConnectionConfig(), client.WithAPIVersionNegotiation())
}

func newWithConfig(ctx context.Context, logger *logger.Logger, cfg connectionConfig, opts ...client.Opt) (*Client, error) {
	backoff := cfg.initialBackoff
	var lastErr error
	for attempt := 0; attempt <= cfg.maxRetries; attempt++ {
		cli, err := client.New(opts...)
		if err == nil {
			attemptCtx, cancel := context.WithTimeout(ctx, cfg.timeout)
			ping, pingErr := cli.Ping(attemptCtx, client.PingOptions{})
			cancel()
			if pingErr == nil {
				attemptCtx, cancel = context.WithTimeout(ctx, cfg.timeout)
				version, versionErr := cli.ServerVersion(attemptCtx, client.ServerVersionOptions{})
				cancel()
				if versionErr == nil {
					logger.Info("Successfully connected to Docker daemon",
						"docker_version", version.Version,
						"api_version", version.APIVersion,
						"os_type", ping.OSType,
						"arch", version.Arch,
						"experimental", ping.Experimental)
					return &Client{cli: cli, logger: logger}, nil
				}
				lastErr = fmt.Errorf("getting Docker version: %w", versionErr)
			} else {
				lastErr = fmt.Errorf("pinging Docker daemon: %w", pingErr)
			}
			_ = cli.Close()
		} else {
			lastErr = fmt.Errorf("creating Docker client: %w", err)
		}

		if attempt == cfg.maxRetries {
			break
		}
		logger.Warn("Failed to connect to Docker daemon, retrying",
			"attempt", attempt+1, "max_retries", cfg.maxRetries, "backoff", backoff, "error", lastErr)
		if err := waitForBackoff(ctx, backoff); err != nil {
			return nil, fmt.Errorf("connecting to Docker daemon: %w", err)
		}
		backoff = min(backoff*2, cfg.maxBackoff)
	}
	if lastErr == nil {
		lastErr = errors.New("no Docker connection attempts were made")
	}
	return nil, fmt.Errorf("connecting to Docker daemon after %d attempts: %w", cfg.maxRetries+1, lastErr)
}

func waitForBackoff(ctx context.Context, duration time.Duration) error {
	timer := time.NewTimer(duration)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}

func (c *Client) Close() error {
	return c.cli.Close()
}

func (c *Client) Ping(ctx context.Context) error {
	_, err := c.cli.Ping(ctx, client.PingOptions{})
	return err
}

func (c *Client) ListContainers(ctx context.Context) ([]lifecycle.Container, error) {
	filters := make(client.Filters).Add("label", "com.github.saltbox.saltbox_managed=true")
	listed, err := c.cli.ContainerList(ctx, client.ContainerListOptions{All: true, Filters: filters})
	if err != nil {
		return nil, fmt.Errorf("listing managed containers: %w", err)
	}

	containers := make([]lifecycle.Container, 0, len(listed.Items))
	for _, summary := range listed.Items {
		name := summary.ID
		for _, candidate := range summary.Names {
			candidate = strings.TrimPrefix(candidate, "/")
			if candidate != "" {
				name = candidate
				break
			}
		}
		inspected, err := c.inspect(ctx, summary.ID)
		if err != nil {
			return nil, fmt.Errorf("loading lifecycle configuration for %s: %w", name, err)
		}
		var stopTimeout *time.Duration
		if inspected.Container.Config != nil && inspected.Container.Config.StopTimeout != nil {
			duration := time.Duration(*inspected.Container.Config.StopTimeout) * time.Second
			stopTimeout = &duration
		}
		containers = append(containers, lifecycle.Container{
			ID:          summary.ID,
			Name:        name,
			Labels:      summary.Labels,
			Running:     summary.State == "running",
			StopTimeout: stopTimeout,
		})
	}
	return containers, nil
}

func (c *Client) InspectContainer(ctx context.Context, containerID string) (lifecycle.ContainerState, error) {
	inspected, err := c.inspect(ctx, containerID)
	if err != nil {
		return lifecycle.ContainerState{}, err
	}

	state := lifecycle.ContainerState{Running: inspected.Container.State.Running}
	if inspected.Container.Config == nil || inspected.Container.Config.Healthcheck == nil {
		state.Healthcheck = lifecycle.HealthcheckNone
	} else if tests := inspected.Container.Config.Healthcheck.Test; len(tests) > 0 && strings.EqualFold(tests[0], "NONE") {
		state.Healthcheck = lifecycle.HealthcheckDisabled
	} else {
		state.Healthcheck = lifecycle.HealthcheckConfigured
	}
	if inspected.Container.State.Health == nil {
		state.Health = lifecycle.HealthNone
	} else {
		state.Health = lifecycle.Health(inspected.Container.State.Health.Status)
	}
	return state, nil
}

func (c *Client) inspect(ctx context.Context, containerID string) (*client.ContainerInspectResult, error) {
	result, err := c.cli.ContainerInspect(ctx, containerID, client.ContainerInspectOptions{})
	if err != nil {
		return nil, fmt.Errorf("inspecting container %s: %w", containerID, err)
	}
	return &result, nil
}

func (c *Client) StartContainer(ctx context.Context, containerID string) error {
	if _, err := c.cli.ContainerStart(ctx, containerID, client.ContainerStartOptions{}); err != nil {
		return fmt.Errorf("starting container %s: %w", containerID, err)
	}
	return nil
}

func (c *Client) StopContainer(ctx context.Context, containerID string, timeout time.Duration) error {
	timeoutSeconds := int(timeout / time.Second)
	if _, err := c.cli.ContainerStop(ctx, containerID, client.ContainerStopOptions{Timeout: &timeoutSeconds}); err != nil {
		return fmt.Errorf("stopping container %s: %w", containerID, err)
	}
	return nil
}
