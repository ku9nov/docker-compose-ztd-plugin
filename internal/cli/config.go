package cli

const (
	DefaultHealthcheckTimeout   = 60
	DefaultNoHealthcheckTimeout = 10
	DefaultWaitAfterHealthy     = 0
	DefaultTraefikConfig        = "traefik/dynamic_conf.yml"
	DefaultProxyType            = "traefik"
)

type Config struct {
	DockerArgs           []string
	ComposeFiles         []string
	EnvFiles             []string
	HealthcheckTimeout   int
	NoHealthcheckTimeout int
	WaitAfterHealthy     int
	TraefikConfigFile    string
	ProxyType            string
	Service              string
	UpDetached           bool
	ShowHelp             bool
}
