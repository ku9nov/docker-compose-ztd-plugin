# Changelog

## 0.5.1

- Added blue-green and canary deployment strategies, including safer rollout flags and deployment guards.
- Added runtime metrics and canary lifetime statistics to improve visibility during deploys.
- Added support for TCP and TLS configuration blocks for broader routing scenarios.
- Improved operational reliability with automatic cleanup, better unhealthy-container logging, and shared failure logs across strategies.
- Improved deployment setup by creating required Traefik config directories and cleaning stale state files on startup.
- Updated docs with clearer descriptions, refreshed README content, and a new systemd usage example.
