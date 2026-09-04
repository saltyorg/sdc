package client

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"time"

	"github.com/saltyorg/sdc/pkg/logger"
)

const maxErrorBodyBytes = 64 << 10

// Client represents an HTTP client for communicating with the controller server
type Client struct {
	baseURL    string
	httpClient *http.Client
	logger     *logger.Logger
	userAgent  string
}

// NewClient creates a new controller client
func NewClient(baseURL string, logger *logger.Logger) *Client {
	return &Client{
		baseURL: baseURL,
		httpClient: &http.Client{
			Timeout: 30 * time.Second,
		},
		logger:    logger,
		userAgent: "sdc-client/1.0",
	}
}

// SetUserAgent sets a custom User-Agent header for requests
func (c *Client) SetUserAgent(userAgent string) {
	c.userAgent = userAgent
}

// JobRequest represents a request to start or stop containers
type JobRequest struct {
	Timeout  int      `json:"timeout"`
	Ignore   []string `json:"ignore,omitempty"`
	Shutdown bool     `json:"shutdown,omitempty"`
}

// JobResponse represents a job creation response
type JobResponse struct {
	ID     string `json:"job_id"`
	Status string `json:"status"`
}

// Job represents a job's full state
type Job struct {
	ID        string            `json:"id"`
	Type      string            `json:"type"`
	Status    string            `json:"status"`
	CreatedAt time.Time         `json:"created_at"`
	StartedAt time.Time         `json:"started_at"`
	EndedAt   time.Time         `json:"ended_at"`
	Timeout   int               `json:"timeout"`
	Ignore    []string          `json:"ignore,omitempty"`
	Started   []string          `json:"started,omitempty"`
	Stopped   []string          `json:"stopped,omitempty"`
	Skipped   []string          `json:"skipped,omitempty"`
	Failed    []string          `json:"failed,omitempty"`
	Outcomes  []ContainerResult `json:"outcomes,omitempty"`
	Error     string            `json:"error,omitempty"`
}

type ContainerResult struct {
	Container string `json:"container"`
	Outcome   string `json:"outcome"`
	Reason    string `json:"reason,omitempty"`
	Error     string `json:"error,omitempty"`
}

type HTTPError struct {
	StatusCode int
	Body       string
}

func (e *HTTPError) Error() string {
	return fmt.Sprintf("request failed with status %d: %s", e.StatusCode, e.Body)
}

// HealthResponse represents the health check response
type HealthResponse struct {
	Status string `json:"status"`
}

// StartContainers submits a job to start containers
func (c *Client) StartContainers(ctx context.Context, timeout int, ignore []string) (*JobResponse, error) {
	req := JobRequest{
		Timeout: timeout,
		Ignore:  ignore,
	}

	var resp JobResponse
	if err := c.post(ctx, "/start", req, &resp); err != nil {
		return nil, err
	}

	c.logger.Info("Start job submitted",
		"job_id", resp.ID,
		"status", resp.Status)

	return &resp, nil
}

// StopContainers submits a job to stop containers
func (c *Client) StopContainers(ctx context.Context, timeout int, ignore []string) (*JobResponse, error) {
	return c.stopContainers(ctx, timeout, ignore, false)
}

// StopContainersForShutdown submits the stop operation that gates helper shutdown.
func (c *Client) StopContainersForShutdown(ctx context.Context, timeout int, ignore []string) (*JobResponse, error) {
	return c.stopContainers(ctx, timeout, ignore, true)
}

func (c *Client) stopContainers(ctx context.Context, timeout int, ignore []string, shutdown bool) (*JobResponse, error) {
	req := JobRequest{
		Timeout:  timeout,
		Ignore:   ignore,
		Shutdown: shutdown,
	}

	var resp JobResponse
	if err := c.post(ctx, "/stop", req, &resp); err != nil {
		return nil, err
	}

	c.logger.Info("Stop job submitted",
		"job_id", resp.ID,
		"status", resp.Status)

	return &resp, nil
}

// GetJob retrieves job status and results
func (c *Client) GetJob(ctx context.Context, jobID string) (*Job, error) {
	var job Job
	if err := c.get(ctx, fmt.Sprintf("/job_status/%s", jobID), &job); err != nil {
		return nil, err
	}

	return &job, nil
}

// WaitForJob waits for a job to complete (completed or failed status)
func (c *Client) WaitForJob(ctx context.Context, jobID string, pollInterval time.Duration) (*Job, error) {
	if pollInterval <= 0 {
		return nil, fmt.Errorf("poll interval must be greater than 0")
	}

	for {
		job, err := c.GetJob(ctx, jobID)
		if err != nil {
			return nil, fmt.Errorf("failed to get job status: %w", err)
		}
		c.logger.Debug("Job status", "job_id", jobID, "status", job.Status)
		if job.Status == "completed" || job.Status == "failed" {
			return job, nil
		}

		timer := time.NewTimer(pollInterval)
		select {
		case <-ctx.Done():
			timer.Stop()
			return nil, ctx.Err()
		case <-timer.C:
		}
	}
}

// Health checks if the server is healthy
func (c *Client) Health(ctx context.Context) error {
	var resp HealthResponse
	if err := c.get(ctx, "/ping", &resp); err != nil {
		return err
	}

	if resp.Status != "healthy" {
		return fmt.Errorf("server is not healthy: %s", resp.Status)
	}

	return nil
}

// Ready checks operational readiness and falls back to the v1 liveness route.
func (c *Client) Ready(ctx context.Context) error {
	var response HealthResponse
	if err := c.get(ctx, "/ready", &response); err != nil {
		var httpErr *HTTPError
		if errors.As(err, &httpErr) && httpErr.StatusCode == http.StatusNotFound {
			return c.Health(ctx)
		}
		return err
	}
	if response.Status != "ready" {
		return fmt.Errorf("server is not ready: %s", response.Status)
	}
	return nil
}

// WaitForServerReady waits for the server to become ready
func (c *Client) WaitForServerReady(ctx context.Context, timeout time.Duration) error {
	c.logger.Info("Waiting for server to become ready",
		"url", c.baseURL,
		"timeout", timeout)

	readyCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	for {
		if err := c.Ready(readyCtx); err == nil {
			c.logger.Info("Server is ready")
			return nil
		} else {
			c.logger.Debug("Server not ready yet", "error", err)
		}
		timer := time.NewTimer(time.Second)
		select {
		case <-readyCtx.Done():
			timer.Stop()
			return fmt.Errorf("timeout waiting for server to become ready: %w", readyCtx.Err())
		case <-timer.C:
		}
	}
}

// post performs a POST request
func (c *Client) post(ctx context.Context, path string, body any, result any) error {
	data, err := json.Marshal(body)
	if err != nil {
		return fmt.Errorf("failed to marshal request: %w", err)
	}

	url := c.baseURL + path
	req, err := http.NewRequestWithContext(ctx, "POST", url, bytes.NewReader(data))
	if err != nil {
		return fmt.Errorf("failed to create request: %w", err)
	}

	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("User-Agent", c.userAgent)

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		bodyBytes, _ := io.ReadAll(io.LimitReader(resp.Body, maxErrorBodyBytes))
		return &HTTPError{StatusCode: resp.StatusCode, Body: string(bodyBytes)}
	}

	if err := json.NewDecoder(resp.Body).Decode(result); err != nil {
		return fmt.Errorf("failed to decode response: %w", err)
	}

	return nil
}

// get performs a GET request
func (c *Client) get(ctx context.Context, path string, result any) error {
	url := c.baseURL + path
	req, err := http.NewRequestWithContext(ctx, "GET", url, nil)
	if err != nil {
		return fmt.Errorf("failed to create request: %w", err)
	}

	req.Header.Set("User-Agent", c.userAgent)

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		bodyBytes, _ := io.ReadAll(io.LimitReader(resp.Body, maxErrorBodyBytes))
		return &HTTPError{StatusCode: resp.StatusCode, Body: string(bodyBytes)}
	}

	if err := json.NewDecoder(resp.Body).Decode(result); err != nil {
		return fmt.Errorf("failed to decode response: %w", err)
	}

	return nil
}
