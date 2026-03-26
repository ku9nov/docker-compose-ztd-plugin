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
docker ztd [OPTIONS] auto-cleanup-run
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
docker ztd auto-cleanup-run
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
- `auto-cleanup-run`: processes overdue cleanup deadlines from state files

Blue-green state:

- stored at `.ztd/state/<compose_project>--<service>.json`
- includes service name, strategy, blue/green container IDs, active color, and optional cleanup deadline
- overdue `cleanupAt` entries are processed as a safety-net on every CLI startup
- overdue entries can also be processed by a scheduler via `auto-cleanup-run`

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

## Auto-cleanup scheduler (Linux)

`--auto-cleanup` only writes cleanup deadlines into state files. To execute cleanup at those deadlines on servers, run `docker ztd auto-cleanup-run` periodically.

### Recommended: systemd timer

By default, the project registry is stored at `~/.ztd/registry/projects.json` for the user running the command. You can override this via `ZTD_REGISTRY_PATH`.

Important: auto-cleanup is user-scoped. Registry entries are isolated per OS user, so if different users run `docker ztd` for different projects, each user needs their own systemd service and timer.

Example service unit (`/etc/systemd/system/ztd-auto-cleanup.service`):

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

Example timer unit (`/etc/systemd/system/ztd-auto-cleanup.timer`):

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

The command uses a non-blocking file lock in `.ztd/state/.auto-cleanup.lock`, so overlapping timer runs are skipped safely.

### Fallback: cron

For hosts without systemd:

```cron
*/5 * * * * cd /srv/my-app && /usr/bin/docker ztd auto-cleanup-run >> /var/log/ztd-auto-cleanup.log 2>&1
```

