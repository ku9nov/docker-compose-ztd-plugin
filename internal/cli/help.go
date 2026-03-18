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

Rolling new Compose service version.

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
      --traefik-conf FILE     Specify Traefik configuration file (default: %s)

`, DefaultHealthcheckTimeout, DefaultNoHealthcheckTimeout, DefaultTraefikConfig)
}
