package traefik

import "testing"

func TestHasTraefikEnableLabel(t *testing.T) {
	cases := []struct {
		name   string
		labels any
		want   bool
	}{
		{
			name:   "array true label",
			labels: []any{"a=b", "traefik.enable=true"},
			want:   true,
		},
		{
			name:   "map true bool",
			labels: map[string]any{"traefik.enable": true},
			want:   true,
		},
		{
			name:   "map true string",
			labels: map[string]any{"traefik.enable": "true"},
			want:   true,
		},
		{
			name:   "missing",
			labels: []any{"a=b"},
			want:   false,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := hasTraefikEnableLabel(tc.labels); got != tc.want {
				t.Fatalf("expected %v, got %v", tc.want, got)
			}
		})
	}
}

func TestSplitEntryPoints(t *testing.T) {
	in := "xmpp, web,  metrics"
	out := splitEntryPoints(in)
	if len(out) != 3 || out[0] != "xmpp" || out[1] != "web" || out[2] != "metrics" {
		t.Fatalf("unexpected entrypoints: %#v", out)
	}
}

func TestCollectTCPRouterMeta_TLSLabel(t *testing.T) {
	labels := map[string]string{
		"traefik.tcp.routers.example-xmpp.rule":                            "HostSNI(`*`)",
		"traefik.tcp.routers.example-xmpp.entrypoints":                     "xmpp-secure",
		"traefik.tcp.routers.example-xmpp.tls":                             "true",
		"traefik.tcp.services.example-xmpp.loadbalancer.server.port":       "5223",
		"traefik.tcp.routers.example-xmpp-plain.rule":                      "HostSNI(`*`)",
		"traefik.tcp.routers.example-xmpp-plain.entrypoints":               "xmpp",
		"traefik.tcp.services.example-xmpp-plain.loadbalancer.server.port": "5222",
	}

	meta := collectTCPRouterMeta(labels)
	if len(meta) != 2 {
		t.Fatalf("expected 2 TCP routers, got %d", len(meta))
	}

	if !meta[0].TLSEnabled {
		t.Fatalf("expected tls enabled for %q router", meta[0].RouterName)
	}
	if meta[1].TLSEnabled {
		t.Fatalf("expected tls disabled for %q router", meta[1].RouterName)
	}
}
