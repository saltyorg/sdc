package main

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/saltyorg/sdc/internal/config"
	"github.com/spf13/cobra"
)

var (
	Version   = "dev"
	GitCommit = "unknown"
	BuildTime = "unknown"
)

type commandRunners struct {
	server func(context.Context, config.ServerConfig) error
	helper func(context.Context, config.HelperConfig) error
}

func newRootCommand(runners commandRunners) *cobra.Command {
	root := &cobra.Command{
		Use:           "saltbox-docker-controller",
		Short:         "Saltbox Docker Container Orchestrator",
		Long:          "A dependency-aware Docker container orchestrator for Saltbox.\nManages container startup/shutdown order based on dependency labels.",
		Version:       fmt.Sprintf("%s (commit: %s, built: %s)", Version, GitCommit, BuildTime),
		SilenceUsage:  true,
		SilenceErrors: true,
	}
	root.CompletionOptions.DisableDefaultCmd = true
	root.AddCommand(newServerCommand(runners.server), newHelperCommand(runners.helper))
	return root
}

func newServerCommand(run func(context.Context, config.ServerConfig) error) *cobra.Command {
	var cfg config.ServerConfig
	command := &cobra.Command{
		Use:   "server",
		Short: "Run the Docker controller API server",
		Long:  "Starts the REST API server that manages Docker container orchestration",
		Args:  cobra.NoArgs,
		RunE: func(command *cobra.Command, _ []string) error {
			if cfg.Host == "" || cfg.Port < 1 || cfg.Port > 65535 {
				return fmt.Errorf("server host and port must be valid")
			}
			return run(command.Context(), cfg)
		},
	}
	command.Flags().StringVar(&cfg.Host, "host", "127.0.0.1", "API server host")
	command.Flags().IntVar(&cfg.Port, "port", 3377, "API server port")
	return command
}

func newHelperCommand(run func(context.Context, config.HelperConfig) error) *cobra.Command {
	var cfg config.HelperConfig
	command := &cobra.Command{
		Use:   "helper",
		Short: "Run in helper mode (Docker lifecycle integration)",
		Long:  "Starts containers when Docker daemon starts and stops them when Docker stops.\nThis mode is designed to run as a systemd service with dependency on docker.service",
		Args:  cobra.NoArgs,
		RunE: func(command *cobra.Command, _ []string) error {
			if cfg.ControllerURL == "" || cfg.StartupDelay < 0 || cfg.Timeout <= 0 || cfg.PollInterval <= 0 {
				return fmt.Errorf("helper URL, timeout, startup delay, and poll interval must be valid")
			}
			return run(command.Context(), cfg)
		},
	}
	command.Flags().StringVar(&cfg.ControllerURL, "controller-url", "http://127.0.0.1:3377", "Controller API URL")
	command.Flags().DurationVar(&cfg.StartupDelay, "startup-delay", 5*time.Second, "Initial delay before starting containers")
	command.Flags().IntVar(&cfg.Timeout, "timeout", 600, "Operation timeout in seconds")
	command.Flags().DurationVar(&cfg.PollInterval, "poll-interval", 5*time.Second, "Job status polling interval")
	return command
}

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	root := newRootCommand(commandRunners{server: runServer, helper: runHelper})
	if err := root.ExecuteContext(ctx); err != nil {
		_, _ = fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}
}
