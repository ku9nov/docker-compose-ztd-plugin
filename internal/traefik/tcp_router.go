package traefik

import "github.com/ku9nov/docker-compose-ztd-plugin/internal/types"

func newTCPRouter(rule string, service string, entryPoints []string, tlsEnabled bool) types.TCPRouter {
	router := types.TCPRouter{
		Rule:    rule,
		Service: service,
	}
	if len(entryPoints) > 0 {
		router.EntryPoints = append([]string{}, entryPoints...)
	}
	if tlsEnabled {
		router.TLS = &struct{}{}
	}
	return router
}
