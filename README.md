# GdayRedis

[![CI](https://github.com/its-the-vibe/GdayRedis/actions/workflows/ci.yaml/badge.svg)](https://github.com/its-the-vibe/GdayRedis/actions/workflows/ci.yaml)

A production-ready "Hello World" Go service with Redis integration, containerised using a distroless Docker image.

## Features

- Prints **Gday World** on startup
- Pings a Redis server at a configurable interval
- Minimal distroless runtime image
- Read-only container filesystem
- Configuration via `config.yaml` + `REDIS_PASSWORD` environment variable

## Prerequisites

- [Go 1.24+](https://go.dev/dl/)
- [Docker](https://docs.docker.com/get-docker/) & [Docker Compose](https://docs.docker.com/compose/)
- An external Redis instance

## Quick Start

### Local

```bash
# 1. Copy and customise configuration
cp config.example.yaml config.yaml
cp .env.example .env

# 2. Edit config.yaml to point to your Redis host/port
# 3. Set REDIS_PASSWORD in .env

# 4. Build and run
make run
```

### Docker

```bash
cp config.example.yaml config.yaml
cp .env.example .env
# Edit config.yaml and .env

make docker-up
```

## Configuration

| File | Purpose |
|------|---------|
| `config.yaml` | Redis host/port and ping interval (git-ignored) |
| `config.example.yaml` | Template – copy to `config.yaml` |
| `.env` | `REDIS_PASSWORD` secret (git-ignored) |
| `.env.example` | Template – copy to `.env` |

### `config.yaml` options

```yaml
redis:
  host: localhost
  port: 6379

ping_interval_seconds: 5
```

## Makefile targets

| Target | Description |
|--------|-------------|
| `make build` | Compile binary to `bin/gdayredis` |
| `make run` | Build and run locally |
| `make test` | Run Go tests |
| `make lint` | Run `go vet` |
| `make docker-build` | Build Docker image |
| `make docker-up` | Start via Docker Compose |
| `make docker-down` | Stop Docker Compose stack |

## Project Layout

```
.
├── cmd/gdayredis/   # Application entry point
├── .github/workflows/ci.yaml
├── config.example.yaml
├── .env.example
├── Dockerfile
├── docker-compose.yml
├── Makefile
├── go.mod / go.sum
└── README.md
```
