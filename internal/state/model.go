package state

import (
	"fmt"
	"regexp"
	"strings"
	"time"
)

const (
	StrategyBlueGreen = "blue-green"
	StrategyCanary    = "canary"

	ColorBlue  = "blue"
	ColorGreen = "green"
)

var validProjectName = regexp.MustCompile(`^[a-zA-Z0-9_.-]+$`)

type QAModes struct {
	Host    string `json:"host,omitempty"`
	Headers string `json:"headers,omitempty"`
	Cookies string `json:"cookies,omitempty"`
	IP      string `json:"ip,omitempty"`
}

type MetricsBaseline struct {
	CapturedAt    time.Time `json:"capturedAt"`
	Requests2xx   float64   `json:"requests2xx"`
	Requests4xx   float64   `json:"requests4xx"`
	Requests5xx   float64   `json:"requests5xx"`
	DurationSum   float64   `json:"durationSum"`
	DurationCount float64   `json:"durationCount"`
}

type DeploymentState struct {
	Service    string           `json:"service"`
	Strategy   string           `json:"strategy"`
	Blue       []string         `json:"blue"`
	Green      []string         `json:"green"`
	Active     string           `json:"active"`
	Old        []string         `json:"old,omitempty"`
	New        []string         `json:"new,omitempty"`
	Weight     int              `json:"weight,omitempty"`
	CreatedAt  time.Time        `json:"createdAt"`
	SwitchedAt *time.Time       `json:"switchedAt,omitempty"`
	CleanupAt  *time.Time       `json:"cleanupAt,omitempty"`
	QA         *QAModes         `json:"qa,omitempty"`
	GreenStats *MetricsBaseline `json:"greenStats,omitempty"`
}

func (s DeploymentState) Validate() error {
	if strings.TrimSpace(s.Service) == "" {
		return fmt.Errorf("state service is empty")
	}
	switch s.Strategy {
	case StrategyBlueGreen:
		if s.Active != ColorBlue && s.Active != ColorGreen {
			return fmt.Errorf("state active must be %s or %s", ColorBlue, ColorGreen)
		}
		if len(s.Blue) == 0 && len(s.Green) == 0 {
			return fmt.Errorf("state must contain at least one color container set")
		}
		switch s.Active {
		case ColorBlue:
			if len(s.Blue) == 0 {
				return fmt.Errorf("state active color %s has no containers", ColorBlue)
			}
		case ColorGreen:
			if len(s.Green) == 0 {
				return fmt.Errorf("state active color %s has no containers", ColorGreen)
			}
		}
	case StrategyCanary:
		if s.Weight < 0 || s.Weight > 100 {
			return fmt.Errorf("state weight must be between 0 and 100 for %s", StrategyCanary)
		}
		if len(s.Old) == 0 && len(s.New) == 0 {
			return fmt.Errorf("state must contain at least one canary container set")
		}
		if s.Weight > 0 && len(s.New) == 0 {
			return fmt.Errorf("canary state with weight %d requires new containers", s.Weight)
		}
		if s.Weight < 100 && len(s.Old) == 0 {
			return fmt.Errorf("canary state with weight %d requires old containers", s.Weight)
		}
	default:
		return fmt.Errorf("state strategy must be %s or %s", StrategyBlueGreen, StrategyCanary)
	}
	return nil
}

func ResolveProjectName(labels map[string]string, fallback string) (string, error) {
	if labels != nil {
		if project := strings.TrimSpace(labels["com.docker.compose.project"]); project != "" {
			if !validProjectName.MatchString(project) {
				return "", fmt.Errorf("invalid compose project name: %s", project)
			}
			return project, nil
		}
	}
	fallback = strings.TrimSpace(fallback)
	if fallback == "" {
		return "", fmt.Errorf("compose project name is missing")
	}
	if !validProjectName.MatchString(fallback) {
		return "", fmt.Errorf("invalid compose project fallback: %s", fallback)
	}
	return fallback, nil
}
