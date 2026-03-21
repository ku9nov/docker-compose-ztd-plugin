# `docker ztd`

Zero-downtime rollout plugin for Docker Compose.

## Overview

`docker ztd` updates a running Compose service without dropping traffic:

- scale service to 2x replicas
- wait for new containers to become ready
- switch Traefik dynamic config to new container IDs
- remove old containers after a drain wait

You can use either implementation:

- Bash implementation (legacy, simple setup for some environments)
- Go implementation (new modular implementation)

## Installation

### Bash implementation

```bash
curl -fsSL https://gist.githubusercontent.com/ku9nov/f76d2b7f65fa266a17c89e0a50880479/raw/9182ae94d16bea270a4228dd17be16f05e156041/install-docker-ztd.sh | bash
```

#### Bash dependencies

You should have `jq` and `yq` installed on your server.

When using the bash-based installation, you may need to create the Traefik folder manually depending on your Docker setup:

```bash
mkdir -p traefik
chown -R $(id -u):$(id -g) traefik
chmod -R 755 traefik
```

### Go implementation

There is no auto-install script for Go yet. Build and install manually:

```bash
go build -o docker-ztd-go ./cmd/docker-ztd
mkdir -p ~/.docker/cli-plugins
cp docker-ztd-go ~/.docker/cli-plugins/docker-ztd
chmod +x ~/.docker/cli-plugins/docker-ztd
```

Go implementation advantage: no runtime dependency on `jq` or `yq`.

## Runtime Dependencies

- Docker CLI
- Compose support:
  - `docker compose` (preferred), or
  - `docker-compose` (fallback)

## Usage

```bash
docker ztd [OPTIONS] SERVICE
docker ztd [OPTIONS] SERVICE ACTION
```

Examples:

```bash
docker ztd -f docker-compose.yml <service-name>
docker ztd -f docker-compose.yml up -d
docker ztd --strategy=blue-green --host-mode=green.example.com api
docker ztd --strategy=blue-green api switch
docker ztd --strategy=blue-green --auto-cleanup=10m api switch
docker ztd --strategy=blue-green api cleanup
docker ztd --strategy=canary --weight=10 api
docker ztd --strategy=canary --weight=70 api
docker ztd --strategy=canary api rollback
docker ztd --strategy=canary --auto-cleanup=10m api rollback
docker ztd --strategy=canary api cleanup
```

Options:

- `-h, --help`
- `-f, --file FILE`
- `-t, --timeout N`
- `-w, --wait N`
- `--wait-after-healthy N`
- `--env-file FILE`
- `--proxy TYPE`
- `--strategy TYPE` (`rolling` default, `blue-green`, `canary`)
- `--traefik-conf FILE`
- `--host-mode VALUE` (blue-green only)
- `--headers-mode HEADER=VALUE` (blue-green only, example: `X-Env=green`)
- `--cookies-mode COOKIE=VALUE` (blue-green only, example: `env=green`)
- `--ip-mode VALUE` (blue-green only)
- `--weight N` (canary only, default: `10`)
- `--to COLOR` (blue-green `switch` action only, `blue|green`)
- `--auto-cleanup DURATION` (`switch`/`rollback` actions only, e.g. `10m`)

Actions:

- `switch` (blue-green only): flips active production traffic between blue and green
- `rollback` (canary only): sets canary traffic to `0` (old receives `100%`)
- `cleanup` (blue-green/canary): removes inactive containers and clears state file

Blue-green state:

- stored at `.ztd/state/<compose_project>--<service>.json`
- includes service name, strategy, blue/green container IDs, active color, and optional cleanup deadline
- overdue `cleanupAt` entries are processed as a safety-net on every CLI startup

Canary state:

- stored in the same state directory and key pattern as blue-green
- includes service name, strategy, old/new container IDs and current canary weight
- cleanup is allowed only for terminal canary traffic states:
  - new=`100` -> remove old
  - new=`0` -> remove new
  - intermediate weights are rejected to preserve rollback safety


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

