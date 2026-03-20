package metricsgate

import (
	"bufio"
	"context"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"
)

type Config struct {
	Enabled          bool
	URL              string
	Window           time.Duration
	Interval         time.Duration
	MinRequests      int
	Max5xxRatio      float64
	Max4xxRatio      float64
	MaxMeanLatencyMS float64
}

type Progress struct {
	Elapsed           time.Duration
	Window            time.Duration
	SuccessfulSamples int
	FailedPolls       int
}

type Verdict string

const (
	VerdictPass             Verdict = "pass"
	VerdictFail             Verdict = "fail"
	VerdictInsufficientData Verdict = "insufficient_data"
)

type Result struct {
	Verdict       Verdict
	Samples       int
	TotalRequests float64
	Ratio2xx      float64
	Ratio4xx      float64
	Ratio5xx      float64
	MeanLatencyMS float64
	Reasons       []string
}

type Snapshot struct {
	Requests2xx   float64
	Requests4xx   float64
	Requests5xx   float64
	DurationSum   float64
	DurationCount float64
}

func (r Result) Summary() string {
	return fmt.Sprintf(
		"verdict=%s samples=%d total=%.0f 2xx=%.2f%% 4xx=%.2f%% 5xx=%.2f%% mean=%.2fms",
		r.Verdict,
		r.Samples,
		r.TotalRequests,
		r.Ratio2xx*100,
		r.Ratio4xx*100,
		r.Ratio5xx*100,
		r.MeanLatencyMS,
	)
}

func Analyze(ctx context.Context, client *http.Client, cfg Config, targetServices []string) (Result, error) {
	return AnalyzeWithProgress(ctx, client, cfg, targetServices, nil)
}

func AnalyzeWithProgress(
	ctx context.Context,
	client *http.Client,
	cfg Config,
	targetServices []string,
	progressFn func(Progress),
) (Result, error) {
	if client == nil {
		client = &http.Client{Timeout: 5 * time.Second}
	}
	if strings.TrimSpace(cfg.URL) == "" {
		return Result{}, fmt.Errorf("metrics URL is required")
	}
	if cfg.Window <= 0 {
		return Result{}, fmt.Errorf("analysis window must be greater than 0")
	}
	if cfg.Interval <= 0 {
		return Result{}, fmt.Errorf("analysis interval must be greater than 0")
	}

	start, err := CaptureSnapshot(ctx, client, cfg.URL, targetServices)
	if err != nil {
		return Result{}, err
	}

	last := start
	successfulSamples := 1
	failedPolls := 0
	startedAt := time.Now()
	deadline := time.Now().Add(cfg.Window)
	ticker := time.NewTicker(cfg.Interval)
	defer ticker.Stop()
	for time.Now().Before(deadline) {
		select {
		case <-ctx.Done():
			return Result{}, ctx.Err()
		case <-ticker.C:
			snapshot, err := CaptureSnapshot(ctx, client, cfg.URL, targetServices)
			if err != nil {
				failedPolls++
				if progressFn != nil {
					progressFn(Progress{
						Elapsed:           time.Since(startedAt),
						Window:            cfg.Window,
						SuccessfulSamples: successfulSamples,
						FailedPolls:       failedPolls,
					})
				}
				continue
			}
			last = snapshot
			successfulSamples++
			if progressFn != nil {
				progressFn(Progress{
					Elapsed:           time.Since(startedAt),
					Window:            cfg.Window,
					SuccessfulSamples: successfulSamples,
					FailedPolls:       failedPolls,
				})
			}
		}
	}

	return EvaluateFromSnapshots(cfg, start, last, successfulSamples), nil
}

func EvaluateFromSnapshots(cfg Config, start Snapshot, end Snapshot, samples int) Result {
	delta := snapshotDelta(start, end)
	total := delta.Requests2xx + delta.Requests4xx + delta.Requests5xx
	result := Result{
		Samples:       samples,
		TotalRequests: total,
	}
	if total > 0 {
		result.Ratio2xx = delta.Requests2xx / total
		result.Ratio4xx = delta.Requests4xx / total
		result.Ratio5xx = delta.Requests5xx / total
	}
	if delta.DurationCount > 0 {
		result.MeanLatencyMS = (delta.DurationSum / delta.DurationCount) * 1000
	}

	if total < float64(cfg.MinRequests) {
		result.Verdict = VerdictInsufficientData
		result.Reasons = append(result.Reasons, fmt.Sprintf("insufficient requests: got %.0f, need at least %d", total, cfg.MinRequests))
		return result
	}

	var reasons []string
	if result.Ratio5xx > cfg.Max5xxRatio {
		reasons = append(reasons, fmt.Sprintf("5xx ratio %.4f exceeds limit %.4f", result.Ratio5xx, cfg.Max5xxRatio))
	}
	if cfg.Max4xxRatio >= 0 && result.Ratio4xx > cfg.Max4xxRatio {
		reasons = append(reasons, fmt.Sprintf("4xx ratio %.4f exceeds limit %.4f", result.Ratio4xx, cfg.Max4xxRatio))
	}
	if cfg.MaxMeanLatencyMS >= 0 && delta.DurationCount > 0 && result.MeanLatencyMS > cfg.MaxMeanLatencyMS {
		reasons = append(reasons, fmt.Sprintf("mean latency %.2fms exceeds limit %.2fms", result.MeanLatencyMS, cfg.MaxMeanLatencyMS))
	}
	result.Reasons = reasons
	if len(reasons) > 0 {
		result.Verdict = VerdictFail
		return result
	}
	result.Verdict = VerdictPass
	return result
}

func snapshotDelta(start, end Snapshot) Snapshot {
	return Snapshot{
		Requests2xx:   nonNegative(end.Requests2xx - start.Requests2xx),
		Requests4xx:   nonNegative(end.Requests4xx - start.Requests4xx),
		Requests5xx:   nonNegative(end.Requests5xx - start.Requests5xx),
		DurationSum:   nonNegative(end.DurationSum - start.DurationSum),
		DurationCount: nonNegative(end.DurationCount - start.DurationCount),
	}
}

func nonNegative(v float64) float64 {
	if v < 0 {
		return 0
	}
	return v
}

func CaptureSnapshot(ctx context.Context, client *http.Client, url string, targets []string) (Snapshot, error) {
	if client == nil {
		client = &http.Client{Timeout: 5 * time.Second}
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return Snapshot{}, err
	}
	resp, err := client.Do(req)
	if err != nil {
		return Snapshot{}, fmt.Errorf("fetch metrics: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		return Snapshot{}, fmt.Errorf("fetch metrics: unexpected status %d", resp.StatusCode)
	}

	targetSet := map[string]struct{}{}
	for _, target := range targets {
		targetSet[strings.TrimSpace(target)] = struct{}{}
	}

	var out Snapshot
	scanner := bufio.NewScanner(resp.Body)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		name, labels, value, ok := parseMetricLine(line)
		if !ok {
			continue
		}
		service := labels["service"]
		if !matchesTargetService(service, targetSet) {
			continue
		}
		switch name {
		case "traefik_service_requests_total":
			switch codeClass(labels["code"]) {
			case 2:
				out.Requests2xx += value
			case 4:
				out.Requests4xx += value
			case 5:
				out.Requests5xx += value
			}
		case "traefik_service_request_duration_seconds_sum":
			out.DurationSum += value
		case "traefik_service_request_duration_seconds_count":
			out.DurationCount += value
		}
	}
	if err := scanner.Err(); err != nil {
		return Snapshot{}, err
	}

	return out, nil
}

func parseMetricLine(line string) (string, map[string]string, float64, bool) {
	idx := strings.LastIndexByte(line, ' ')
	if idx <= 0 {
		return "", nil, 0, false
	}
	head := strings.TrimSpace(line[:idx])
	valueRaw := strings.TrimSpace(line[idx+1:])
	value, err := strconv.ParseFloat(valueRaw, 64)
	if err != nil {
		return "", nil, 0, false
	}

	if open := strings.IndexByte(head, '{'); open >= 0 {
		close := strings.LastIndexByte(head, '}')
		if close < open {
			return "", nil, 0, false
		}
		return head[:open], parseLabels(head[open+1 : close]), value, true
	}
	return head, map[string]string{}, value, true
}

func parseLabels(raw string) map[string]string {
	out := map[string]string{}
	if strings.TrimSpace(raw) == "" {
		return out
	}
	parts := strings.Split(raw, ",")
	for _, part := range parts {
		pair := strings.SplitN(strings.TrimSpace(part), "=", 2)
		if len(pair) != 2 {
			continue
		}
		key := strings.TrimSpace(pair[0])
		value := strings.Trim(pair[1], `"`)
		out[key] = value
	}
	return out
}

func matchesTargetService(service string, targets map[string]struct{}) bool {
	if len(targets) == 0 {
		return true
	}
	if _, ok := targets[service]; ok {
		return true
	}
	base := service
	if at := strings.IndexByte(service, '@'); at >= 0 {
		base = service[:at]
	}
	_, ok := targets[base]
	return ok
}

func codeClass(code string) int {
	if len(code) != 3 {
		return 0
	}
	switch code[0] {
	case '2':
		return 2
	case '4':
		return 4
	case '5':
		return 5
	default:
		return 0
	}
}
