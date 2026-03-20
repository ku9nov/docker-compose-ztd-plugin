package cli

import "time"

const (
	DefaultHealthcheckTimeout   = 60
	DefaultNoHealthcheckTimeout = 10
	DefaultWaitAfterHealthy     = 0
	DefaultTraefikConfig        = "traefik/dynamic_conf.yml"
	DefaultProxyType            = "traefik"
	DefaultStrategy             = StrategyRolling
	DefaultCanaryWeight         = 10
)

const (
	StrategyRolling   = "rolling"
	StrategyBlueGreen = "blue-green"
	StrategyCanary    = "canary"
)

const (
	ActionDeploy   = ""
	ActionSwitch   = "switch"
	ActionCleanup  = "cleanup"
	ActionPromote  = "promote"
	ActionRollback = "rollback"
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
	Strategy             string
	HostMode             string
	HeadersMode          string
	CookiesMode          string
	IPMode               string
	Weight               int
	Action               string
	SwitchTo             string
	AutoCleanup          time.Duration
	Service              string
	UpDetached           bool
	ShowHelp             bool
}
