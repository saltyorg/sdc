package main

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"time"

	"github.com/saltyorg/sdc/internal/client"
	"github.com/saltyorg/sdc/internal/config"
	"github.com/saltyorg/sdc/pkg/logger"
)

const helperReadinessTimeout = 60 * time.Second

type helperClient interface {
	WaitForServerReady(context.Context, time.Duration) error
	StartContainers(context.Context, int, []string) (*client.JobResponse, error)
	StopContainersForShutdown(context.Context, int, []string) (*client.JobResponse, error)
	WaitForJob(context.Context, string, time.Duration) (*client.Job, error)
}

func runHelper(ctx context.Context, cfg config.HelperConfig) error {
	log, err := logger.New(false)
	if err != nil {
		return fmt.Errorf("initializing logger: %w", err)
	}
	apiClient := client.NewClient(cfg.ControllerURL, log)
	apiClient.SetUserAgent("sdc/" + Version)
	return executeHelper(ctx, cfg, apiClient, log)
}

func executeHelper(ctx context.Context, cfg config.HelperConfig, apiClient helperClient, log *logger.Logger) error {
	startupErr := runHelperStartup(ctx, cfg, apiClient, log)
	if startupErr != nil && ctx.Err() == nil {
		return startupErr
	}
	if ctx.Err() == nil {
		<-ctx.Done()
	}
	return runHelperShutdown(ctx, cfg, apiClient, log)
}

func runHelperStartup(ctx context.Context, cfg config.HelperConfig, apiClient helperClient, log *logger.Logger) error {
	if err := apiClient.WaitForServerReady(ctx, helperReadinessTimeout); err != nil {
		return fmt.Errorf("waiting for controller readiness: %w", err)
	}
	if cfg.StartupDelay > 0 {
		timer := time.NewTimer(cfg.StartupDelay)
		defer timer.Stop()
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-timer.C:
		}
	}

	response, err := apiClient.StartContainers(ctx, cfg.Timeout, nil)
	if err != nil {
		if isOperationBlocked(err) {
			log.Info("Container start operation is currently blocked, skipping")
			return nil
		}
		return fmt.Errorf("submitting start job: %w", err)
	}
	job, err := apiClient.WaitForJob(ctx, response.ID, cfg.PollInterval)
	if err != nil {
		return fmt.Errorf("waiting for start job: %w", err)
	}
	if job.Status == "failed" {
		log.Error("Container start job failed", "error", job.Error, "failed", job.Failed)
	} else {
		log.Info("Containers started", "started", job.Started, "skipped", job.Skipped)
	}
	return nil
}

func runHelperShutdown(signalCtx context.Context, cfg config.HelperConfig, apiClient helperClient, log *logger.Logger) error {
	timeout := time.Duration(cfg.Timeout)*time.Second + helperReadinessTimeout
	ctx, cancel := context.WithTimeout(context.WithoutCancel(signalCtx), timeout)
	defer cancel()
	if err := apiClient.WaitForServerReady(ctx, helperReadinessTimeout); err != nil {
		return fmt.Errorf("waiting for controller before shutdown: %w", err)
	}
	response, err := apiClient.StopContainersForShutdown(ctx, cfg.Timeout, nil)
	if err != nil {
		if isOperationBlocked(err) {
			log.Info("Container stop operation is currently blocked, skipping")
			return nil
		}
		return fmt.Errorf("submitting shutdown stop job: %w", err)
	}
	job, err := apiClient.WaitForJob(ctx, response.ID, cfg.PollInterval)
	if err != nil {
		return fmt.Errorf("waiting for shutdown stop job: %w", err)
	}
	if job.Status == "failed" {
		return fmt.Errorf("shutdown stop job failed: %s", job.Error)
	}
	log.Info("Containers stopped", "stopped", job.Stopped, "skipped", job.Skipped)
	return nil
}

func isOperationBlocked(err error) bool {
	var httpErr *client.HTTPError
	return errors.As(err, &httpErr) && httpErr.StatusCode == http.StatusServiceUnavailable
}
