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
       docker ztd [OPTIONS] auto-cleanup-run

Rolling new Compose service version.

Actions:
  switch                    blue-green only: switch active traffic between blue and green
  rollback                  canary only: route 100%% traffic to old containers
  cleanup                   blue-green/canary: cleanup inactive side and clear state
  auto-cleanup-run          process overdue cleanup deadlines from state files

Options:
  General:
    -h, --help                  Print usage
    -f, --file FILE             Compose configuration files
        --env-file FILE         Specify an alternate environment file
    -t, --timeout N             Healthcheck timeout (default: %d seconds)
    -w, --wait N                When no healthcheck is defined, wait for N seconds
                                before stopping old container (default: %d seconds)
        --wait-after-healthy N  When healthcheck is defined and succeeds, wait for additional N seconds
                                before stopping the old container (default: 0 seconds)
        --strategy TYPE         Deployment strategy (default: %s, options: rolling, blue-green, canary)
        --proxy TYPE            Set proxy type (default: traefik, options: traefik, nginx-proxy)
        --traefik-conf FILE     Specify Traefik configuration file (default: %s)

  Blue-green:
        --host-mode VALUE       Route by host (HTTP Host / TCP HostSNI, example: green.example.com)
        --headers-mode VALUE    Route by header (HTTP only, example: X-Env=green)
        --cookies-mode VALUE    Route by cookie (HTTP only, example: env=green)
        --ip-mode VALUE         Route by client IP CIDR (HTTP/TCP, example: 10.0.0.0/24)
        --to COLOR              switch action target color (blue|green)

  Canary:
        --weight N              canary mode (default: %d)

  Action-specific:
        --auto-cleanup DURATION switch/rollback actions only (example: 10m, 1h30m)

  Runtime analysis:
        --analyze               Enable runtime metrics analysis for blue-green/canary
        --metrics-url URL       Traefik metrics endpoint (default: %s)
        --analyze-window DUR    Observation window for metrics delta (default: %s)
        --analyze-interval DUR  Poll interval during analysis window (default: %s)
        --min-requests N        Minimum request count for verdict (default: %d)
        --max-5xx-ratio N       Maximum allowed 5xx ratio [0..1] (default: %.2f)
        --max-4xx-ratio N       Maximum allowed 4xx ratio [0..1], -1 disables (default: %.2f)
        --max-mean-latency-ms N Maximum allowed mean latency in milliseconds, -1 disables (default: %.2f)

`, DefaultHealthcheckTimeout, DefaultNoHealthcheckTimeout, DefaultStrategy, DefaultTraefikConfig, DefaultCanaryWeight, DefaultMetricsURL, DefaultAnalyzeWindow, DefaultAnalyzeInterval, DefaultAnalyzeMinRequests, DefaultAnalyzeMax5xxRatio, DefaultAnalyzeMax4xxRatio, DefaultAnalyzeMaxLatencyMS)
}
