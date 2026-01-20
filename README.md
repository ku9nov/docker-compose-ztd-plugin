# <code>docker ztd</code> 🚀
## Zero Downtime Deployment for Docker Compose

`docker ztd` is a CLI plugin for Docker that allows updating Docker Compose services without downtime using dynamic Traefik configurations.

### Why is this important? 🤔
Updating services in production without downtime is crucial for high-load applications. Instead of using `docker compose up -d <service>`, which can cause downtime, `docker ztd`:
- **Scales** the service to double the number of containers ✅
- **Waits** for the new containers to be ready 🕐
- **Removes** old containers without traffic loss 🔄

## 📥 Installation

```bash
curl -fsSL https://gist.githubusercontent.com/ku9nov/f76d2b7f65fa266a17c89e0a50880479/raw/9182ae94d16bea270a4228dd17be16f05e156041/install-docker-ztd.sh | bash
```

### Dependencies

You should have installed `jq` and `yq` on your server.

Depends on how Docker is installed. In some systems, you may need to create the Traefik folder manually.

```bash
mkdir -p traefik
chown -R $(id -u):$(id -g) traefik
chmod -R 755 traefik
```

## 🛠 Usage

:warning: Your service cannot have `container_name` and `ports` defined in `docker-compose.yml`, as it's not possible to run multiple containers with the same name or port mapping.

Simply replace `docker compose up -d <service>` with `docker ztd <service>`.

```bash
docker ztd -f docker-compose.yml <service-name>
```

Or start all services:

```bash
# It is recommended to use this only once to prevent uncontrolled container recreation, for example, due to label updates. All service updates should be performed exclusively using "docker ztd <service-name>".
docker ztd -f docker-compose.yml up -d
```

## 🔧 Adding Traefik to `docker-compose.yml`
To ensure `docker ztd` works correctly, configure `traefik`:

```yaml
services:
  traefik-proxy:
    image: traefik:v3.3.4
    command:
      - --api.insecure=true
      - --entrypoints.web.address=:80
      - --providers.file.directory=/etc/traefik # it's important to use a directory due to a Traefik bug
      - --providers.file.watch=true
      # XMPP TCP EXAMPLE
      - --entrypoints.xmpp.address=:5222
    restart: unless-stopped
    ports:
      - "8000:80"
      - "8080:8080"
      # XMPP TCP EXAMPLE
      - "5222:5222"
    volumes:
      - ./traefik:/etc/traefik:ro
    networks:
      - traefik

networks:
  traefik:
    driver: bridge
```

## 📌 Adding Traefik Labels for Services
For each service that needs to be updated without downtime, add the standard Traefik labels:

```yaml
services:
  helloworld-front:
    image: <image>
    labels:
      - "traefik.enable=true"
      - "traefik.http.routers.helloworld-front.entrypoints=web" # required
      - "traefik.http.routers.helloworld-front.rule=Host(`frontend.example.com`)" # required
      - "traefik.http.services.helloworld-front.loadbalancer.server.port=80" # required
      # XMPP TCP EXAMPLE
      - traefik.tcp.routers.example-xmpp.rule=HostSNI(`*`)
      - traefik.tcp.routers.example-xmpp.entrypoints=xmpp
      - traefik.tcp.services.example-xmpp.loadbalancer.server.port=5222
```

## 🎯 How Does It Work?

Zero Downtime Deployment is achieved through **automatic generation of dynamic configurations** for Traefik. By default, this file is stored at:

```bash
traefik/dynamic_conf.yml
```

## 🔗 Links
📂 Example configuration repository: [docker-compose-deployments-examples](https://github.com/ku9nov/docker-compose-deployments-examples)

## ⚠️ Limitations
The `docker ztd` plugin **does not support full configuration generation** for all Traefik parameters, only a specific subset:

- `traefik.enable`
- `traefik.http.routers.<name>.entrypoints`
- `traefik.http.routers.<name>.rule`
- `traefik.http.services.<name>.loadbalancer.server.port`
- `traefik.http.services.<name>.loadbalancer.healthCheck.path` 
- `traefik.http.services.<name>.loadbalancer.healthCheck.interval`
- `traefik.http.services.<name>.loadbalancer.healthCheck.timeout`
- `traefik.http.services.<name>.loadbalancer.healthCheck.scheme`
- `traefik.http.services.<name>.loadbalancer.healthCheck.mode`
- `traefik.http.services.<name>.loadbalancer.healthCheck.hostname`
- `traefik.http.services.<name>.loadbalancer.healthCheck.port`
- `traefik.http.services.<name>.loadbalancer.healthCheck.headers.My-Custom-Header`
- `traefik.http.services.<name>.loadbalancer.healthCheck.followRedirects`
- `traefik.http.services.<name>.loadbalancer.healthCheck.method`
- `traefik.http.services.<name>.loadbalancer.healthCheck.status`
- `traefik.tcp.routers.<name>.rule`
- `traefik.tcp.routers.<name>.entrypoints`
- `traefik.tcp.services.<name>.loadbalancer.server.port`

## 🛣 Roadmap 🏗
- [ ] Add support for `nginx-proxy` 🔜
- [ ] Implement **blue-green** and **canary deployments** 🎯

🚀 Use `docker ztd` to update services without downtime and improve production reliability! 😎

