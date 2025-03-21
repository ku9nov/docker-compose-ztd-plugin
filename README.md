<h1 align="center">
<code>docker ztd</code><br>
Zero Downtime Deployment for Docker Compose
</h1>
#### inspired by [`docker-rollout`](https://github.com/wowu/docker-rollout)

Docker CLI plugin that updates Docker Compose services without downtime using traefik dynamic configurations.

Simply replace `docker compose up -d <service>` with `docker ztd <service>` in your deployment scripts. This command will scale the service to twice the current number of instances, wait for the new containers to be ready, and then remove the old containers.

## Installation

```bash
curl -fsSL https://gist.githubusercontent.com/ku9nov/f76d2b7f65fa266a17c89e0a50880479/raw/9182ae94d16bea270a4228dd17be16f05e156041/install-docker-ztd.sh | bash
```

## Usage

Run `docker ztd <name>` instead of `docker compose up -d <name>` to update a service without downtime. If you have both `docker compose` plugin and `docker-compose` command available, docker-ztd will use `docker compose` by default.

```bash
docker ztd -f docker-compose.yml <service-name>
```