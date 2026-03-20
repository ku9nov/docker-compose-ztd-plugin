package cli

import "fmt"

func PluginMetadata() string {
	return `{
  "SchemaVersion": "0.1.0",
  "Vendor": "Serhii Kuianov",
  "Version": "v0.5",
  "ShortDescription": "Docker plugin for docker-compose update with real zero time deployment."
}
`
}

func Usage() string {
	return fmt.Sprintf(`
Usage: docker ztd [OPTIONS] SERVICE
       docker ztd [OPTIONS] SERVICE ACTION

Rolling new Compose service version.

Actions:
  switch                    blue-green only: switch active traffic between blue and green
  promote                   canary only: change canary weight to --weight
  rollback                  canary only: route 100%% traffic to old containers
  cleanup                   blue-green/canary: cleanup inactive side and clear state

Options:
  -h, --help                  Print usage
  -f, --file FILE             Compose configuration files
  -t, --timeout N             Healthcheck timeout (default: %d seconds)
  -w, --wait N                When no healthcheck is defined, wait for N seconds
                              before stopping old container (default: %d seconds)
      --wait-after-healthy N  When healthcheck is defined and succeeds, wait for additional N seconds
                              before stopping the old container (default: 0 seconds)
      --env-file FILE         Specify an alternate environment file
      --proxy TYPE            Set proxy type (default: traefik, options: traefik, nginx-proxy)
      --strategy TYPE         Deployment strategy (default: %s, options: rolling, blue-green, canary)
      --traefik-conf FILE     Specify Traefik configuration file (default: %s)
      --host-mode VALUE       blue-green mode (example: green.example.com)
      --headers-mode VALUE    blue-green mode (example: X-Env=green)
      --cookies-mode VALUE    blue-green mode (example: env=green)
      --ip-mode VALUE         blue-green mode (example: 10.0.0.0/24)
      --weight N              canary mode (default: %d)
      --to COLOR              switch action target color (blue|green)
      --auto-cleanup DURATION switch/promote/rollback actions only (example: 10m, 1h30m)

`, DefaultHealthcheckTimeout, DefaultNoHealthcheckTimeout, DefaultStrategy, DefaultTraefikConfig, DefaultCanaryWeight)
}
