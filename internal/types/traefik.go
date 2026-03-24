package types

type DynamicConfig struct {
	HTTP *HTTPConfig `yaml:"http,omitempty"`
	TCP  *TCPConfig  `yaml:"tcp,omitempty"`
}

type HTTPConfig struct {
	Routers  map[string]HTTPRouter  `yaml:"routers,omitempty"`
	Services map[string]HTTPService `yaml:"services,omitempty"`
}

type HTTPRouter struct {
	Rule     string `yaml:"rule,omitempty"`
	Service  string `yaml:"service,omitempty"`
	Priority int    `yaml:"priority,omitempty"`
}

type HTTPService struct {
	LoadBalancer *HTTPLoadBalancer  `yaml:"loadBalancer,omitempty"`
	Weighted     *HTTPWeightedRoute `yaml:"weighted,omitempty"`
}

type HTTPLoadBalancer struct {
	Servers     []HTTPServer  `yaml:"servers,omitempty"`
	HealthCheck *HealthChecks `yaml:"healthCheck,omitempty"`
}

type HTTPWeightedRoute struct {
	Services []HTTPWeightedService `yaml:"services,omitempty"`
}

type HTTPWeightedService struct {
	Name   string `yaml:"name,omitempty"`
	Weight int    `yaml:"weight,omitempty"`
}

type HTTPServer struct {
	URL string `yaml:"url,omitempty"`
}

type HealthChecks struct {
	Path            string            `yaml:"path,omitempty"`
	Interval        string            `yaml:"interval,omitempty"`
	Timeout         string            `yaml:"timeout,omitempty"`
	Scheme          string            `yaml:"scheme,omitempty"`
	Mode            string            `yaml:"mode,omitempty"`
	Hostname        string            `yaml:"hostname,omitempty"`
	Port            string            `yaml:"port,omitempty"`
	FollowRedirects string            `yaml:"followRedirects,omitempty"`
	Method          string            `yaml:"method,omitempty"`
	Status          string            `yaml:"status,omitempty"`
	Headers         map[string]string `yaml:"headers,omitempty"`
}

type TCPConfig struct {
	Routers  map[string]TCPRouter  `yaml:"routers,omitempty"`
	Services map[string]TCPService `yaml:"services,omitempty"`
}

type TCPRouter struct {
	Rule        string   `yaml:"rule,omitempty"`
	Service     string   `yaml:"service,omitempty"`
	EntryPoints []string `yaml:"entryPoints,omitempty"`
}

type TCPService struct {
	LoadBalancer *TCPLoadBalancer  `yaml:"loadBalancer,omitempty"`
	Weighted     *TCPWeightedRoute `yaml:"weighted,omitempty"`
}

type TCPLoadBalancer struct {
	Servers []TCPServer `yaml:"servers,omitempty"`
}

type TCPWeightedRoute struct {
	Services []TCPWeightedService `yaml:"services,omitempty"`
}

type TCPWeightedService struct {
	Name   string `yaml:"name,omitempty"`
	Weight int    `yaml:"weight,omitempty"`
}

type TCPServer struct {
	Address string `yaml:"address,omitempty"`
}
