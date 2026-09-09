# QueryLab

[![CI](https://github.com/its-the-vibe/QueryLab/actions/workflows/ci.yaml/badge.svg)](https://github.com/its-the-vibe/QueryLab/actions/workflows/ci.yaml)

A web front-end service for discovering and executing GoQuery queries via Poppit.

## Features

- Discovers executable queries at startup using `./goquery --json list`
- Lets users run queries from a web UI using `./goquery --json query <query_name>`
- Renders query output in a tabular format
- Configuration via `config.yaml` and optional env overrides
- Container-ready (Dockerfile included)

## Prerequisites

- [Go 1.24+](https://go.dev/dl/)
- [Docker](https://docs.docker.com/get-docker/) & [Docker Compose](https://docs.docker.com/compose/)
- A local clone/build of [Poppit](https://github.com/its-the-vibe/Poppit) with `goquery` executable available

## Quick Start

### Local

```bash
# 1. Copy and customise configuration
cp config.example.yaml config.yaml
cp .env.example .env

# 2. Edit config.yaml and set poppit.directory to your Poppit path

# 3. Build and run
make run
```

### Docker

```bash
cp config.example.yaml config.yaml
cp .env.example .env
# Edit config.yaml and .env if needed

make docker-up
```

## Configuration

| File | Purpose |
|------|---------|
| `config.yaml` | Runtime configuration (git-ignored) |
| `config.example.yaml` | Template – copy to `config.yaml` |
| `.env` | Sensitive environment variables (git-ignored) |
| `.env.example` | Template – copy to `.env` |

### `config.yaml` options

```yaml
server:
  addr: ":8080"
poppit:
  directory: "/path/to/poppit"
```

## Makefile targets

| Target | Description |
|--------|-------------|
| `make build` | Compile binary to `bin/querylab` |
| `make run` | Build and run locally |
| `make test` | Run Go tests |
| `make lint` | Run `go vet` |
| `make docker-build` | Build Docker image |
| `make docker-up` | Start via Docker Compose |
| `make docker-down` | Stop Docker Compose stack |

## Project Layout

```
.
├── cmd/querylab/   # Web service entry point
├── .github/workflows/ci.yaml
├── config.example.yaml
├── .env.example
├── Dockerfile
├── docker-compose.yml
├── Makefile
├── go.mod / go.sum
└── README.md
```
