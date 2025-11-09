# Saltbox Docker Controller (SDC)

A dependency-aware Docker container orchestrator for Saltbox, written in Go.

## Overview

SDC manages Docker container startup and shutdown based on dependency labels. It ensures containers start in the correct order, waits for health checks, and handles graceful shutdown in reverse dependency order.

**Features:**
- Dependency-aware orchestration using Docker labels
- Topological sort for optimal startup/shutdown order
- Parallel execution of independent containers
- Health check polling with configurable timeouts
- Job-based API for async operations
- REST API server mode
- Helper mode for Docker daemon lifecycle integration
- Graceful shutdown handling
- Comprehensive test suite (54 tests)

## Building

Build using the Makefile:

```bash
make build
```

The binary will be created at `build/sdc`.

For all available targets:
```bash
make help
```

## Testing

Run all tests:
```bash
make test
```

Run tests with coverage:
```bash
make test-coverage
```

## Usage

### Server Mode
Start the REST API server:
```bash
./build/sdc server --host 127.0.0.1 --port 3377
```

Or use the Makefile:
```bash
make run-server
```

### Helper Mode
Run the helper daemon for automatic lifecycle management:
```bash
./build/sdc helper --controller-url http://127.0.0.1:3377
```

Or use the Makefile:
```bash
make run-helper
```

### Version Information
```bash
./build/sdc --version
```

## Architecture

### Package Structure

```
sdc/
├── cmd/controller/         # Main entry point (server/helper commands)
├── internal/
│   ├── api/               # HTTP handlers, middleware, and router
│   ├── client/            # HTTP client for helper mode
│   ├── docker/            # Docker client wrapper and label parsing
│   ├── graph/             # Dependency graph and topological sort
│   ├── orchestrator/      # Container orchestration engine
│   ├── jobs/              # Job manager with worker pool
│   └── config/            # Configuration management
└── pkg/logger/            # Structured logging (Zap)
```

### How It Works

1. **Label-based dependencies**: Containers declare dependencies via `sdc.requires` labels
2. **Graph building**: SDC builds a dependency graph from all running containers
3. **Topological sort**: Determines optimal startup/shutdown order
4. **Batch execution**: Independent containers in each batch start/stop in parallel
5. **Health checking**: Polls container health status before proceeding to dependents
6. **Job tracking**: All operations are tracked as jobs with UUID and status

## Dependencies

- `github.com/moby/moby/client` v0.1.0-beta.3 - Docker client SDK
- `github.com/moby/moby/api` v1.52.0-beta.4 - Docker API types
- `github.com/go-chi/chi/v5` - HTTP router
- `go.uber.org/zap` - Structured logging
- `github.com/spf13/cobra` - CLI framework
- `github.com/google/uuid` - UUID generation
- `github.com/stretchr/testify` - Testing utilities

## Docker Labels

SDC uses Docker labels to define container dependencies:

```yaml
labels:
  sdc.requires: "postgres,redis"  # Comma-separated list of container names
  sdc.startup_delay: "5"          # Seconds to wait after this container starts
```

**Example docker-compose.yml:**
```yaml
services:
  postgres:
    image: postgres:15
    labels:
      sdc.startup_delay: "10"

  redis:
    image: redis:7
    labels:
      sdc.startup_delay: "5"

  app:
    image: myapp:latest
    labels:
      sdc.requires: "postgres,redis"
      sdc.startup_delay: "2"
```

SDC will ensure `postgres` and `redis` start first (in parallel), wait for health checks, apply startup delays, then start `app`.

## API Endpoints

### Container Operations
- `POST /start` - Start containers in dependency order
  - Request: `{"timeout": 600, "ignore": ["container1"]}`
  - Response: `{"id": "uuid", "status": "pending"}`
- `POST /stop` - Stop containers in reverse dependency order
  - Request: `{"timeout": 300, "ignore": ["container2"]}`
  - Response: `{"id": "uuid", "status": "pending"}`

### Job Management
- `GET /jobs` - List all jobs
- `GET /jobs/{id}` - Get job status and results
- `DELETE /jobs/{id}` - Delete a job

### Health
- `GET /health` - Health check endpoint

## Helper Mode Details

The helper mode is designed to run as a systemd service for automatic container lifecycle management:

**Lifecycle:**
1. Wait for controller server to become ready (60s timeout)
2. Apply configured startup delay
3. Submit start job for all managed containers
4. Wait for job completion
5. Run until receiving SIGTERM/SIGINT
6. Submit stop job for all containers
7. Wait for stop completion and exit gracefully

**Options:**
```bash
./build/sdc helper \
  --controller-url http://localhost:3377 \  # Controller API URL
  --startup-delay 10s \                      # Delay before starting containers
  --timeout 600 \                            # Job timeout in seconds
  --poll-interval 5s                         # Status polling interval
```

## License

GNU General Public License v3.0
