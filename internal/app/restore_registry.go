package app

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/ku9nov/docker-compose-ztd-plugin/internal/cli"
	"github.com/ku9nov/docker-compose-ztd-plugin/internal/configio"
)

const registryPathEnv = "ZTD_RESTORE_REGISTRY_PATH"

type restoreRegistry struct {
	Projects map[string]restoreProject `json:"projects"`
}

type restoreProject struct {
	ID                string    `json:"id"`
	WorkingDir        string    `json:"workingDir"`
	ComposeFiles      []string  `json:"composeFiles"`
	EnvFiles          []string  `json:"envFiles,omitempty"`
	DockerArgs        []string  `json:"dockerArgs,omitempty"`
	TraefikConfigFile string    `json:"traefikConfigFile,omitempty"`
	Enabled           bool      `json:"enabled"`
	UpdatedAt         time.Time `json:"updatedAt"`
}

func registerCurrentProjectForRestore(cfg cli.Config) (string, string, error) {
	workingDir, err := os.Getwd()
	if err != nil {
		return "", "", err
	}

	composeFiles, err := absolutePathsFrom(workingDir, cfg.ComposeFiles)
	if err != nil {
		return "", "", err
	}
	envFiles, err := absolutePathsFrom(workingDir, cfg.EnvFiles)
	if err != nil {
		return "", "", err
	}

	entry := restoreProject{
		WorkingDir:        workingDir,
		ComposeFiles:      composeFiles,
		EnvFiles:          envFiles,
		DockerArgs:        append([]string{}, cfg.DockerArgs...),
		TraefikConfigFile: cfg.TraefikConfigFile,
		Enabled:           true,
		UpdatedAt:         time.Now().UTC(),
	}
	entry.ID = restoreProjectID(entry)

	candidates := registryPathCandidates()
	registry, preferredPath, err := readRegistry(candidates)
	if err != nil {
		return "", "", err
	}
	if registry.Projects == nil {
		registry.Projects = map[string]restoreProject{}
	}
	registry.Projects[entry.ID] = entry

	path, err := writeRegistry(candidates, preferredPath, registry)
	if err != nil {
		return "", "", err
	}
	return entry.ID, path, nil
}

func loadRegisteredProjects() ([]restoreProject, string, error) {
	candidates := registryPathCandidates()
	registry, path, err := readRegistry(candidates)
	if err != nil {
		return nil, "", err
	}
	projects := make([]restoreProject, 0, len(registry.Projects))
	for _, entry := range registry.Projects {
		projects = append(projects, entry)
	}
	sort.Slice(projects, func(i, j int) bool {
		return projects[i].ID < projects[j].ID
	})
	return projects, path, nil
}

func removeRegisteredProjects(ids []string) error {
	if len(ids) == 0 {
		return nil
	}
	candidates := registryPathCandidates()
	registry, preferredPath, err := readRegistry(candidates)
	if err != nil {
		return err
	}
	if len(registry.Projects) == 0 {
		return nil
	}
	for _, id := range ids {
		delete(registry.Projects, id)
	}
	_, err = writeRegistry(candidates, preferredPath, registry)
	return err
}

func registryPathCandidates() []string {
	if configured := strings.TrimSpace(os.Getenv(registryPathEnv)); configured != "" {
		return []string{configured}
	}

	candidates := []string{"/var/lib/docker-ztd/registry.json"}
	if stateHome := strings.TrimSpace(os.Getenv("XDG_STATE_HOME")); stateHome != "" {
		candidates = append(candidates, filepath.Join(stateHome, "docker-ztd", "registry.json"))
	}
	if home, err := os.UserHomeDir(); err == nil && strings.TrimSpace(home) != "" {
		candidates = append(candidates, filepath.Join(home, ".local", "state", "docker-ztd", "registry.json"))
	}
	return uniqueStrings(candidates)
}

func readRegistry(candidates []string) (restoreRegistry, string, error) {
	empty := restoreRegistry{Projects: map[string]restoreProject{}}
	for _, path := range candidates {
		data, err := os.ReadFile(path)
		if err != nil {
			if os.IsNotExist(err) {
				continue
			}
			return restoreRegistry{}, "", err
		}

		var registry restoreRegistry
		if err := json.Unmarshal(data, &registry); err != nil {
			return restoreRegistry{}, "", fmt.Errorf("failed to parse restore registry %s: %w", path, err)
		}
		if registry.Projects == nil {
			registry.Projects = map[string]restoreProject{}
		}
		return registry, path, nil
	}
	if len(candidates) == 0 {
		return empty, "", nil
	}
	return empty, candidates[0], nil
}

func writeRegistry(candidates []string, preferred string, registry restoreRegistry) (string, error) {
	paths := reorderCandidates(candidates, preferred)
	data, err := configio.MarshalJSON(registry)
	if err != nil {
		return "", err
	}

	var lastErr error
	for _, path := range paths {
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			lastErr = err
			continue
		}
		if err := configio.WriteAtomic(path, data, 0o644); err != nil {
			lastErr = err
			continue
		}
		return path, nil
	}
	if lastErr != nil {
		return "", lastErr
	}
	return "", fmt.Errorf("no registry path candidates available")
}

func reorderCandidates(candidates []string, preferred string) []string {
	ordered := make([]string, 0, len(candidates))
	if strings.TrimSpace(preferred) != "" {
		ordered = append(ordered, preferred)
	}
	for _, candidate := range candidates {
		if candidate == preferred {
			continue
		}
		ordered = append(ordered, candidate)
	}
	return uniqueStrings(ordered)
}

func uniqueStrings(values []string) []string {
	set := map[string]struct{}{}
	out := make([]string, 0, len(values))
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" {
			continue
		}
		if _, exists := set[value]; exists {
			continue
		}
		set[value] = struct{}{}
		out = append(out, value)
	}
	return out
}

func absolutePathsFrom(baseDir string, paths []string) ([]string, error) {
	out := make([]string, 0, len(paths))
	for _, p := range paths {
		p = strings.TrimSpace(p)
		var target string
		if filepath.IsAbs(p) {
			target = filepath.Clean(p)
		} else {
			target = filepath.Join(baseDir, p)
		}
		abs, err := filepath.Abs(target)
		if err != nil {
			return nil, err
		}
		out = append(out, abs)
	}
	return out, nil
}

func restoreProjectID(project restoreProject) string {
	normalized := strings.Join([]string{
		project.WorkingDir,
		strings.Join(project.ComposeFiles, "|"),
		strings.Join(project.EnvFiles, "|"),
		strings.Join(project.DockerArgs, "|"),
	}, "\x00")
	sum := sha256.Sum256([]byte(normalized))
	return hex.EncodeToString(sum[:12])
}

func withWorkingDirectory(targetDir string, fn func() error) error {
	currentDir, err := os.Getwd()
	if err != nil {
		return err
	}
	if err := os.Chdir(targetDir); err != nil {
		return err
	}
	defer os.Chdir(currentDir)
	return fn()
}
