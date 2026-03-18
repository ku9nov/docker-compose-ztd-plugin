# `docker ztd`

Zero-downtime rollout plugin for Docker Compose.

## Overview

`docker ztd` updates a running Compose service without dropping traffic:

- scale service to 2x replicas
- wait for new containers to become ready
- switch Traefik dynamic config to new container IDs
- remove old containers after a drain wait


## Build and Install

```bash
go build -o docker-ztd-go ./cmd/docker-ztd
mkdir -p ~/.docker/cli-plugins
cp docker-ztd-go ~/.docker/cli-plugins/docker-ztd
chmod +x ~/.docker/cli-plugins/docker-ztd
```

## Runtime Dependencies

- Docker CLI
- Compose support:
  - `docker compose` (preferred), or
  - `docker-compose` (fallback)

## Usage

```bash
docker ztd [OPTIONS] SERVICE
```

Examples:

```bash
docker ztd -f docker-compose.yml <service-name>
docker ztd -f docker-compose.yml up -d
```

Options:

- `-h, --help`
- `-f, --file FILE`
- `-t, --timeout N`
- `-w, --wait N`
- `--wait-after-healthy N`
- `--env-file FILE`
- `--proxy TYPE`
- `--traefik-conf FILE`


## Traefik Labels Supported

- `traefik.enable`
- `traefik.http.routers.<name>.rule`
- `traefik.http.services.<name>.loadbalancer.server.port`
- `traefik.http.services.<name>.loadbalancer.healthCheck.path`
- `traefik.http.services.<name>.loadbalancer.healthCheck.interval`
- `traefik.http.services.<name>.loadbalancer.healthCheck.timeout`
- `traefik.http.services.<name>.loadbalancer.healthCheck.scheme`
- `traefik.http.services.<name>.loadbalancer.healthCheck.mode`
- `traefik.http.services.<name>.loadbalancer.healthCheck.hostname`
- `traefik.http.services.<name>.loadbalancer.healthCheck.port`
- `traefik.http.services.<name>.loadbalancer.healthCheck.headers.<header>`
- `traefik.http.services.<name>.loadbalancer.healthCheck.followRedirects`
- `traefik.http.services.<name>.loadbalancer.healthCheck.method`
- `traefik.http.services.<name>.loadbalancer.healthCheck.status`
- `traefik.tcp.routers.<name>.rule`
- `traefik.tcp.routers.<name>.entrypoints`
- `traefik.tcp.services.<name>.loadbalancer.server.port`

## Notes

- Avoid `container_name` and fixed host `ports` on services that need multi-replica rollout.
- `nginx-proxy` mode remains not implemented.

