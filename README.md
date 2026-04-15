# `docker ztd`

Zero-downtime deployment orchestration plugin for Docker Compose with `rolling`, `blue-green`, and `canary` strategies.

> [!IMPORTANT]
> `docker ztd` requires a `traefik` service in your `docker-compose.yml`.
> The plugin will not work without Traefik present in the Compose project.

`docker ztd` updates a running Compose service without dropping traffic:

- scales service to additional replicas
- waits for new containers to become ready
- switches Traefik dynamic config to new container IDs
- removes old containers after a drain wait

## Example Deployments (Recommended)

For complete, ready-to-run `helloworld` examples and deployment notes, see:

[docker-compose-deployments-examples](https://github.com/ku9nov/docker-compose-deployments-examples#)

## Quick Start (Go Implementation, Recommended)

The Go implementation is the recommended path for most users.

### 1) Install

```bash
curl -fsSL https://raw.githubusercontent.com/ku9nov/docker-compose-ztd-plugin/main/scripts/install-docker-ztd-go.sh | bash
```

### 2) Run your first deployment

```bash
docker ztd -f docker-compose.yml up -d
```

### 3) Pick a strategy

```bash
# Rolling (default)
docker ztd -f docker-compose.yml api

# Blue-green
docker ztd -f docker-compose.yml --strategy=blue-green --host-mode=green.example.com api
docker ztd -f docker-compose.yml --strategy=blue-green api switch

# Canary
docker ztd -f docker-compose.yml --strategy=canary --weight=10 api
docker ztd -f docker-compose.yml --strategy=canary --weight=70 api
```

## Runtime Dependencies

- Docker CLI
- Traefik service in the same Compose project (`traefik`)
- Compose support:
  - `docker compose` (preferred), or
  - `docker-compose` (fallback)

## Usage

Always pass `-f docker-compose.yml` (or your custom compose file path).

```bash
docker ztd -f docker-compose.yml [OPTIONS] SERVICE
docker ztd -f docker-compose.yml [OPTIONS] SERVICE ACTION
docker ztd auto-cleanup-run
```

## Strategy Examples

### Rolling

```bash
docker ztd -f docker-compose.yml api
```

### Blue-green

```bash
docker ztd -f docker-compose.yml --strategy=blue-green --host-mode=green.example.com api
docker ztd -f docker-compose.yml --strategy=blue-green api switch
docker ztd -f docker-compose.yml --strategy=blue-green --auto-cleanup=10m api switch
docker ztd -f docker-compose.yml --strategy=blue-green api cleanup
```

### Canary

```bash
docker ztd -f docker-compose.yml --strategy=canary --weight=10 api
docker ztd -f docker-compose.yml --strategy=canary --weight=70 api
docker ztd -f docker-compose.yml --strategy=canary api rollback
docker ztd -f docker-compose.yml --strategy=canary --auto-cleanup=10m api rollback
docker ztd -f docker-compose.yml --strategy=canary api cleanup
```

### Cleanup runner

```bash
docker ztd auto-cleanup-run
```

## Actions

- `switch` (blue-green only): switch active traffic between blue and green
- `rollback` (canary only): route `100%` traffic to old containers
- `cleanup` (blue-green/canary): remove inactive containers and clear state
- `auto-cleanup-run`: process overdue cleanup deadlines from state files

## Options Reference

### General

- `-h, --help`
- `-f, --file FILE`
- `--env-file FILE`
- `-t, --timeout N`
- `-w, --wait N`
- `--wait-after-healthy N`
- `--strategy TYPE` (`rolling` default, `blue-green`, `canary`)
- `--proxy TYPE` (`traefik` default, `nginx-proxy`)
- `--traefik-conf FILE`

### Blue-green

- `--host-mode VALUE` (route by host, HTTP Host / TCP HostSNI)
- `--headers-mode HEADER=VALUE` (HTTP only, example: `X-Env=green`)
- `--cookies-mode COOKIE=VALUE` (HTTP only, example: `env=green`)
- `--ip-mode VALUE` (route by client IP CIDR, HTTP/TCP)
- `--to COLOR` (`switch` action only, `blue|green`)

### Canary

- `--weight N` (default: `10`)

### Action-specific

- `--auto-cleanup DURATION` (`switch`/`rollback` actions only, example: `10m`)

### Runtime analysis

- `--analyze`
- `--metrics-url URL`
- `--analyze-window DURATION`
- `--analyze-interval DURATION`
- `--min-requests N`
- `--max-5xx-ratio N`
- `--max-4xx-ratio N` (`-1` disables)
- `--max-mean-latency-ms N` (`-1` disables)

## State Files

State files are stored at:

- `.ztd/state/<compose_project>--<service>.json`

### Blue-green state

- stores service, strategy, blue/green container IDs, active color, and optional cleanup deadline
- overdue `cleanupAt` entries are processed as a safety net on CLI startup
- overdue entries can also be processed by scheduler via `auto-cleanup-run`

### Canary state

- stores service, strategy, old/new container IDs, and current canary weight
- `cleanup` is allowed only for terminal canary states:
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
- `traefik.tcp.routers.<name>.tls`
- `traefik.tcp.services.<name>.loadbalancer.server.port`

## Operations: Auto-cleanup Scheduler (Linux)

`--auto-cleanup` writes cleanup deadlines into state files. To execute cleanup at those deadlines, run `docker ztd auto-cleanup-run` periodically.

By default, project registry is stored at `~/.ztd/registry/projects.json` for the current OS user. You can override it with `ZTD_REGISTRY_PATH`.

Auto-cleanup is user-scoped. If different users run `docker ztd` for different projects, each user needs their own scheduler.

### Recommended: systemd timer

Service unit (`/etc/systemd/system/ztd-auto-cleanup.service`):

```ini
[Unit]
Description=Run docker ztd overdue auto-cleanup
After=docker.service
Wants=docker.service

[Service]
Type=oneshot
User=deploy
Environment=ZTD_REGISTRY_PATH=~/.ztd/registry/projects.json
ExecStart=/usr/bin/docker ztd auto-cleanup-run
```

Timer unit (`/etc/systemd/system/ztd-auto-cleanup.timer`):

```ini
[Unit]
Description=Schedule docker ztd overdue auto-cleanup

[Timer]
OnCalendar=*:0/1
Persistent=true
Unit=ztd-auto-cleanup.service

[Install]
WantedBy=timers.target
```

Enable:

```bash
sudo systemctl daemon-reload
sudo systemctl enable --now ztd-auto-cleanup.timer
sudo systemctl status ztd-auto-cleanup.timer
```

Logs:

```bash
journalctl -u ztd-auto-cleanup.service -f
```

To avoid overlapping runs, the command uses a non-blocking file lock at `.ztd/state/.auto-cleanup.lock`.

### Fallback: cron

For hosts without systemd:

```cron
*/5 * * * * /usr/bin/docker ztd auto-cleanup-run >> /var/log/ztd-auto-cleanup.log 2>&1
```

## Legacy Bash Implementation (Minimal)

Bash implementation is still available for legacy environments, but Go implementation is recommended for new setups.

Install:

```bash
curl -fsSL https://gist.githubusercontent.com/ku9nov/f76d2b7f65fa266a17c89e0a50880479/raw/9182ae94d16bea270a4228dd17be16f05e156041/install-docker-ztd.sh | bash
```

Dependencies:

- `jq`
- `yq`

## Notes

- Avoid `container_name` and fixed host `ports` on services that need multi-replica rollout.
- `nginx-proxy` mode is not implemented yet.

