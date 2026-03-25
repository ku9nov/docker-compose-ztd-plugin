package traefik

import (
	"sort"
	"strings"
)

type tcpRouterMeta struct {
	RouterName      string
	Rule            string
	RouterService   string
	BackendPort     string
	BackendBaseName string
	EntryPoints     []string
}

type TCPRouteInput struct {
	RouterName      string
	Rule            string
	RouterService   string
	BackendPort     string
	BackendBaseName string
	EntryPoints     []string
}

func collectTCPRouterMeta(labels map[string]string) []tcpRouterMeta {
	names := tcpRouterNames(labels)
	routers := make([]tcpRouterMeta, 0, len(names))
	for _, name := range names {
		rule := labels["traefik.tcp.routers."+name+".rule"]
		routerService := labels["traefik.tcp.routers."+name+".service"]
		if routerService == "" {
			routerService = name
		}
		port := labels["traefik.tcp.services."+routerService+".loadbalancer.server.port"]
		if rule == "" || port == "" {
			continue
		}
		routers = append(routers, tcpRouterMeta{
			RouterName:      name,
			Rule:            rule,
			RouterService:   routerService,
			BackendPort:     port,
			BackendBaseName: normalizeTCPServiceBaseName(routerService),
			EntryPoints:     splitEntryPoints(labels["traefik.tcp.routers."+name+".entrypoints"]),
		})
	}
	return routers
}

func ExtractTCPRoutes(labels map[string]string) []TCPRouteInput {
	meta := collectTCPRouterMeta(labels)
	routes := make([]TCPRouteInput, 0, len(meta))
	for _, item := range meta {
		routes = append(routes, TCPRouteInput{
			RouterName:      item.RouterName,
			Rule:            item.Rule,
			RouterService:   item.RouterService,
			BackendPort:     item.BackendPort,
			BackendBaseName: item.BackendBaseName,
			EntryPoints:     append([]string{}, item.EntryPoints...),
		})
	}
	return routes
}

func normalizeTCPServiceBaseName(name string) string {
	name = strings.TrimSpace(name)
	name = strings.TrimSuffix(name, "-blue")
	name = strings.TrimSuffix(name, "-green")
	name = strings.TrimSuffix(name, "_old")
	name = strings.TrimSuffix(name, "_new")
	return name
}

func tcpRouterNames(labels map[string]string) []string {
	set := map[string]struct{}{}
	for key := range labels {
		if strings.HasPrefix(key, "traefik.tcp.routers.") {
			parts := strings.Split(key, ".")
			if len(parts) >= 4 {
				set[parts[3]] = struct{}{}
			}
		}
	}

	names := make([]string, 0, len(set))
	for n := range set {
		names = append(names, n)
	}
	sort.Strings(names)
	return names
}

func splitEntryPoints(raw string) []string {
	if strings.TrimSpace(raw) == "" {
		return nil
	}
	parts := strings.Split(raw, ",")
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		p = strings.TrimSpace(p)
		if p != "" {
			out = append(out, p)
		}
	}
	return out
}
