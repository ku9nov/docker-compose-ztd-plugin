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
	DefaultMetricsURL           = "http://localhost:8080/metrics"
	DefaultAnalyzeWindow        = 30 * time.Second
	DefaultAnalyzeInterval      = 5 * time.Second
	DefaultAnalyzeMinRequests   = 50
	DefaultAnalyzeMax5xxRatio   = 0.05
	DefaultAnalyzeMax4xxRatio   = -1.0
	DefaultAnalyzeMaxLatencyMS  = -1.0
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
	Analyze              bool
	MetricsURL           string
	AnalyzeWindow        time.Duration
	AnalyzeInterval      time.Duration
	AnalyzeMinRequests   int
	AnalyzeMax5xxRatio   float64
	AnalyzeMax4xxRatio   float64
	AnalyzeMaxLatencyMS  float64
}
