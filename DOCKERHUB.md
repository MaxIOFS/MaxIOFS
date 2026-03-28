# MaxIOFS

Self-hosted S3-compatible object storage — single binary, no external dependencies, batteries included.

## Quick Start

```bash
docker run -d \
  --name maxiofs \
  --restart unless-stopped \
  -p 8080:8080 \
  -p 8081:8081 \
  -v /srv/maxiofs:/data \
  maxiofs/maxiofs:latest
```

- **Web Console:** http://localhost:8081 — default login: `admin` / `admin`
- **S3 API:** http://localhost:8080

## Docker Compose

```bash
# Download docker-compose.yaml from the repository, then:
docker compose up -d
```

Pulls `maxiofs/maxiofs:latest` from DockerHub automatically. Data and config are stored in `./maxiofs-data/` next to the compose file.

## Configuration

On first run, MaxIOFS creates `/data/config.yaml` from a built-in template. With a bind mount this file is directly accessible on the host — edit it and restart to apply changes:

```bash
# With the docker run example above:
nano /srv/maxiofs/config.yaml
docker restart maxiofs

# With Docker Compose:
nano ./maxiofs-data/config.yaml
docker compose restart maxiofs
```

The config file controls encryption at rest, SMTP, public URLs, TLS, and other static settings.

## Ports

| Port | Description |
|------|-------------|
| 8080 | S3-compatible API |
| 8081 | Web Console & REST API |

## Available Tags

| Tag | Description |
|-----|-------------|
| `latest` | Latest stable release |
| `1.x.x` | Specific version |
| `nightly` | Latest nightly build (may be unstable) |

## Platforms

`linux/amd64` and `linux/arm64`

## Documentation

Full documentation, deployment guide, and configuration reference:
https://github.com/MaxioFS/MaxioFS
