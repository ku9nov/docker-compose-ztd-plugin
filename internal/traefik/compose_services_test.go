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
