# Stack Context

Generated: 2026-09-04

## Stack
- **Language**: Go 1.25.3 (`github.com/saltyorg/sdc`)
- **Framework**: Cobra 1.10 CLI, Chi 5.2 HTTP API, Moby Docker client; no database layer
- **Build**: GNU Make wrapping `CGO_ENABLED=0 go build`; output is `build/sdc`
- **Test**: `make test` (`testing` plus Testify); co-located unit tests and benchmarks
- **Quality gate**: `make check` runs formatting, module tidiness, vet, tests, race tests, lifecycle coverage, lint, module verification, and build
- **Static check**: `make vet` and `make tidy-check` [CI gate: yes]
- **Lint**: pinned golangci-lint toolchain via `make lint` [CI gate: yes]
- **Format**: `make fmt-check` using `gofmt -l` [CI gate: yes]

## Secondary Languages
- YAML (GitHub Actions workflow)
- Make (build and verification automation)
- Markdown (project documentation)

## Conventions
- Error handling: early returns, contextual `fmt.Errorf(... %w)`, and `errors.Is` for sentinels
- Module structure: executable in `cmd/controller`; application domains in `internal/*`; logger in `pkg/logger`
- CLI/config: typed config structs bound to package-level Cobra commands and flags in `init`
- Naming: standard Go casing; `New*` constructors, `Handle*` HTTP handlers, and typed option/result structs
- Logging: injected `*logger.Logger` wrapper using key-value fields
- Tests: `*_test.go` beside source; table-driven subtests and Testify assertions; benchmarks in graph/jobs

## CI Gates
- `make check` in the prepare job, including race detection and an 80% lifecycle coverage floor
- Linux cross-builds for amd64, arm64, and ARM v5/v6/v7 after prepare succeeds
- Module content verification and a local controller build are part of the prepare gate
