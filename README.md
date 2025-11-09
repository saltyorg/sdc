# Saltbox Docker Controller (SDC)

A dependency-aware Docker container orchestrator for Saltbox, written in Go.

## Project Status

**Phase 1: Core Infrastructure** - ✅ Complete

- [x] Go module initialization
- [x] Cobra CLI with server/helper modes
- [x] Configuration management via flags
- [x] Structured logging with Zap
- [x] Docker client wrapper (moby/moby beta SDK)
- [x] Container label parsing
- [x] Unit tests
- [x] Static binary build with version injection

**Phase 2: Dependency Graph & Topological Sort** - ✅ Complete

- [x] Graph node structure with dependency tracking
- [x] Dependency graph builder from container labels
- [x] Topological sort algorithm (DFS-based)
- [x] Circular dependency detection
- [x] Placeholder nodes for missing dependencies
- [x] Startup/shutdown batch calculation for parallel operations
- [x] Comprehensive graph tests (16 test cases)

**Phase 3: Orchestration Engine** - ✅ Complete

- [x] Orchestrator with health check logic
- [x] Container start/stop operations with proper ordering
- [x] Health check polling (2s interval, 60s timeout)
- [x] Startup delay support between dependencies
- [x] Batch-based parallel execution
- [x] Ignore list support for skipping containers
- [x] Comprehensive error handling and result tracking
- [x] Orchestrator tests

**Phase 4: Job Management & REST API** - ✅ Complete

- [x] Job types and structures (Job, JobStatus, JobType)
- [x] Job manager with UUID tracking and lifecycle management
- [x] Worker pool for background operations (configurable workers)
- [x] Job retention policy (1 hour minimum, 1000 job limit, LRU eviction)
- [x] REST API endpoints (POST /api/v1/jobs/start, POST /api/v1/jobs/stop)
- [x] Job status and listing endpoints (GET /api/v1/jobs, GET /api/v1/jobs/{id})
- [x] Job deletion endpoint (DELETE /api/v1/jobs/{id})
- [x] Health check endpoint (GET /health)
- [x] Comprehensive job system tests (18 test cases)
- [x] Integration with orchestrator
- [x] Graceful shutdown support

**Phase 5: Helper Mode & Client** - ✅ Complete

- [x] HTTP client for communicating with controller (internal/client)
- [x] Server readiness check with configurable timeout
- [x] Job submission and status polling
- [x] WaitForJob helper for synchronous operations
- [x] Helper mode implementation with lifecycle integration
- [x] Automatic container start on daemon startup
- [x] Automatic container stop on shutdown signal
- [x] Configurable startup delay and poll interval
- [x] Comprehensive client tests (13 test cases)
- [x] Full integration of server and helper modes

**Phase 6: Operations & Shutdown Handling** - ✅ Complete

- [x] Structured request logging middleware with duration tracking
- [x] Recovery middleware for panic handling
- [x] Graceful shutdown handling in server mode (30s timeout)
- [x] Graceful shutdown handling in helper mode
- [x] Signal handling (SIGINT, SIGTERM) in both modes
- [x] Health check implementation with fallback behavior
- [x] Performance benchmarks for graph operations
- [x] Performance benchmarks for job operations

## Building

Build using the Makefile:

```bash
make build
```

The binary will be created at `build/saltbox-docker-controller`.

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
```bash
make run-server
# Or run the built binary:
./build/saltbox-docker-controller server --host 127.0.0.1 --port 3377
```

### Helper Mode
```bash
make run-helper
# Or run the built binary:
./build/saltbox-docker-controller helper --controller-url http://127.0.0.1:3377
```

### Version Information
```bash
make version
# Or run the built binary:
./build/saltbox-docker-controller --version
```

## Architecture

See [golang-rewrite-plan.md](golang-rewrite-plan.md) for the complete implementation plan.

### Package Structure

```
saltbox-docker-controller/
├── cmd/controller/         # Main entry point and commands
├── internal/
│   ├── api/               # HTTP handlers and router ✅
│   ├── client/            # Helper mode HTTP client ✅
│   ├── docker/            # Docker client wrapper ✅
│   ├── graph/             # Dependency graph ✅
│   ├── orchestrator/      # Container orchestration ✅
│   ├── jobs/              # Job management ✅
│   └── config/            # Configuration ✅
└── pkg/logger/            # Logging setup ✅
```

## Dependencies

- `github.com/moby/moby/client` v0.1.0-beta.3 - Docker client SDK
- `github.com/moby/moby/api` v1.52.0-beta.4 - Docker API types
- `github.com/go-chi/chi/v5` - HTTP router
- `go.uber.org/zap` - Structured logging
- `github.com/spf13/cobra` - CLI framework
- `github.com/google/uuid` - UUID generation
- `github.com/stretchr/testify` - Testing utilities

## API Endpoints

The REST API provides the following endpoints:

### Job Management
- `POST /api/v1/jobs/start` - Start containers in dependency order
  - Request body: `{"timeout": 600, "ignore": ["container1"]}`
  - Returns: `{"id": "uuid", "status": "pending"}`
- `POST /api/v1/jobs/stop` - Stop containers in reverse dependency order
  - Request body: `{"timeout": 300, "ignore": ["container2"]}`
  - Returns: `{"id": "uuid", "status": "pending"}`
- `GET /api/v1/jobs` - List all jobs
- `GET /api/v1/jobs/{id}` - Get job status and results
- `DELETE /api/v1/jobs/{id}` - Delete a job

### Health
- `GET /health` - Health check endpoint

## Helper Mode Usage

The helper mode is designed to run as a systemd service and automatically manage container lifecycles:

```bash
# Start in helper mode (waits for server, then starts containers)
./build/saltbox-docker-controller helper --controller-url http://127.0.0.1:3377

# With custom settings
./build/saltbox-docker-controller helper \
  --controller-url http://localhost:3377 \
  --startup-delay 10s \
  --timeout 600 \
  --poll-interval 5s
```

The helper will:
1. Wait for the controller server to become ready (60s timeout)
2. Apply the configured startup delay
3. Submit a job to start all managed containers
4. Wait for the job to complete
5. Run until receiving SIGTERM/SIGINT
6. Submit a job to stop all containers
7. Wait for stop to complete and exit gracefully

## License

GNU General Public License v3.0
