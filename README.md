# dae-wing

**A lightweight REST/OpenAPI control-plane for [dae](https://github.com/daeuniverse/dae)** — the high-performance eBPF-based proxy solution.

[![License](https://img.shields.io/github/license/daeuniverse/dae-wing?style=flat-square&color=blue)](LICENSE)
[![Go Version](https://img.shields.io/github/go-mod/go-version/daeuniverse/dae-wing?style=flat-square)](go.mod)
[![Release](https://img.shields.io/github/v/release/daeuniverse/dae-wing?style=flat-square)](https://github.com/daeuniverse/dae-wing/releases)
[![GitHub Stars](https://img.shields.io/github/stars/daeuniverse/dae-wing?style=flat-square)](https://github.com/daeuniverse/dae-wing/stargazers)

---

## ✨ Features

- 🚀 **REST/OpenAPI API** — Typed control-plane APIs for managing dae
- 🔄 **Hot Reload** — Switch configs without restarting
- 📦 **Subscription Management** — Import and manage proxy subscriptions
- 🐳 **Docker Ready** — Easy deployment with Docker/Docker Compose
- 🔌 **Extensible** — Perfect backend for building custom dashboards

## 📋 Prerequisites

| Dependency                       | Version | Required |
| -------------------------------- | ------- | -------- |
| [Go](https://go.dev)             | >= 1.22 | ✅       |
| [Clang](https://clang.llvm.org)  | >= 15   | ✅       |
| [LLVM](https://llvm.org)         | >= 15   | ✅       |
| [Git](https://git-scm.com)       | Latest  | ✅       |
| [Docker](https://www.docker.com) | Latest  | Optional |

## 🚀 Quick Start

### Clone the Repository

```bash
git clone https://github.com/daeuniverse/dae-wing
cd dae-wing
git submodule update --init --recursive
```

### Run Locally

**API Only Mode** (for development):

```bash
make deps
go run . run -c ./ --api-only
```

**Full Mode** (with dae proxy):

```bash
make deps
go run -exec sudo . run
```

### Run with Docker

Pull the prebuilt image:

```bash
docker pull ghcr.io/daeuniverse/dae-wing
```

Or build from source:

```bash
# Using Docker Compose (recommended)
docker compose up -d

# Or using Docker CLI
docker build -t dae-wing .
docker run -d \
    --privileged \
    --network=host \
    --pid=host \
    --restart=always \
    -v /sys:/sys \
    -v /etc/dae-wing:/etc/dae-wing \
    --name=dae-wing \
    dae-wing
```

## 📖 API Documentation

dae-wing serves its control-plane API under `/api/`.

### OpenAPI

```bash
go build -o dae-wing
curl http://localhost:2023/api/openapi.json
./dae-wing export openapi > openapi.json
```

### Runtime API

Common endpoints include:

1. `GET /api/openapi.json`
2. `GET /api/general/state`
3. `GET /api/runtime/overview`
4. `POST /api/runtime/reload`
5. `POST /api/runtime/stop`

### Export Config Outline

```bash
./dae-wing export outline > outline.json
```

> 💡 **Tip:** Use [dae-outline2config](https://github.com/daeuniverse/dae-outline2config) to convert outlines to dae config format.

## 🏗️ Architecture

### Config

Configs include `global`, `dns`, and `routing` sections from [dae](https://github.com/daeuniverse/dae).

- **Multiple Configs** — Switch between different configurations
- **Shared Resources** — Nodes, subscriptions, and groups are shared across configs
- **Hot Reload** — Selecting a new config automatically reloads dae

### Subscription

A subscription contains:

- Source link (URL)
- Collection of resolved nodes

### Node

Nodes represent proxy profiles imported via links. They can exist:

- Independently (manually added)
- Within subscriptions (auto-imported)

> ⚠️ Nodes are deduplicated by link within the same collection.

### Group

Groups serve as routing outbounds with:

- A collection of subscriptions and nodes
- Node selection policy for connections
- Preserved nodes during subscription updates

## 🤝 Contributing

Contributions are welcome! Please feel free to submit a Pull Request.

## 📄 License

This project is licensed under the [AGPL-3.0 License](LICENSE).

---

<p align="center">
  Made with ❤️ by the <a href="https://github.com/daeuniverse">dae universe</a> team
</p>
