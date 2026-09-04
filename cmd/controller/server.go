package main

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"time"

	"github.com/saltyorg/sdc/internal/api"
	"github.com/saltyorg/sdc/internal/config"
	"github.com/saltyorg/sdc/internal/docker"
	"github.com/saltyorg/sdc/internal/lifecycle"
	"github.com/saltyorg/sdc/pkg/logger"
)

func runServer(ctx context.Context, cfg config.ServerConfig) error {
	log, err := logger.New(false)
	if err != nil {
		return fmt.Errorf("initializing logger: %w", err)
	}
	defer log.Sync()

	dockerClient, err := docker.New(ctx, log)
	if err != nil {
		return fmt.Errorf("creating Docker client: %w", err)
	}
	defer dockerClient.Close()

	controller := lifecycle.NewController(dockerClient, log)
	server := &http.Server{
		Addr:              fmt.Sprintf("%s:%d", cfg.Host, cfg.Port),
		Handler:           api.NewServer(controller, log).Router(),
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       30 * time.Second,
		WriteTimeout:      30 * time.Second,
		IdleTimeout:       120 * time.Second,
	}

	log.Info("Starting Saltbox Docker Controller", "version", Version, "commit", GitCommit, "build_time", BuildTime, "addr", server.Addr)
	serverErrors := make(chan error, 1)
	go func() { serverErrors <- server.ListenAndServe() }()

	select {
	case err := <-serverErrors:
		if errors.Is(err, http.ErrServerClosed) {
			return nil
		}
		return fmt.Errorf("serving controller API: %w", err)
	case <-ctx.Done():
	}

	shutdownCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 30*time.Second)
	defer cancel()
	httpErr := server.Shutdown(shutdownCtx)
	lifecycleErr := controller.Shutdown(shutdownCtx)
	if err := errors.Join(httpErr, lifecycleErr); err != nil {
		return fmt.Errorf("shutting down controller: %w", err)
	}
	return nil
}
