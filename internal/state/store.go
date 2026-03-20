package state

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/ku9nov/docker-compose-ztd-plugin/internal/configio"
)

const DefaultStateDir = ".ztd/state"

type Store struct {
	baseDir string
}

func NewStore(baseDir string) *Store {
	if strings.TrimSpace(baseDir) == "" {
		baseDir = DefaultStateDir
	}
	return &Store{baseDir: baseDir}
}

func ServiceStateKey(project string, service string) (string, error) {
	project = strings.TrimSpace(project)
	service = strings.TrimSpace(service)
	if project == "" {
		return "", fmt.Errorf("project name is empty")
	}
	if service == "" {
		return "", fmt.Errorf("service name is empty")
	}
	if !validProjectName.MatchString(project) {
		return "", fmt.Errorf("project name contains unsupported characters: %s", project)
	}
	if !validProjectName.MatchString(service) {
		return "", fmt.Errorf("service name contains unsupported characters: %s", service)
	}
	return project + "--" + service, nil
}

func (s *Store) Path(project string) (string, error) {
	project = strings.TrimSpace(project)
	if project == "" {
		return "", fmt.Errorf("project name is empty")
	}
	if !validProjectName.MatchString(project) {
		return "", fmt.Errorf("project name contains unsupported characters: %s", project)
	}
	return filepath.Join(s.baseDir, project+".json"), nil
}

func (s *Store) Save(project string, state DeploymentState) error {
	if err := state.Validate(); err != nil {
		return err
	}
	path, err := s.Path(project)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	data, err := json.MarshalIndent(state, "", "  ")
	if err != nil {
		return err
	}
	return configio.WriteAtomic(path, data, 0o644)
}

func (s *Store) Load(project string) (DeploymentState, error) {
	path, err := s.Path(project)
	if err != nil {
		return DeploymentState{}, err
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return DeploymentState{}, err
	}
	var state DeploymentState
	if err := json.Unmarshal(data, &state); err != nil {
		return DeploymentState{}, fmt.Errorf("failed to parse state file %s: %w", path, err)
	}
	if err := state.Validate(); err != nil {
		return DeploymentState{}, fmt.Errorf("invalid state in %s: %w", path, err)
	}
	return state, nil
}

func (s *Store) Delete(project string) error {
	path, err := s.Path(project)
	if err != nil {
		return err
	}
	if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
		return err
	}
	return nil
}

func (s *Store) DeleteByServiceNames(services []string) (int, error) {
	if len(services) == 0 {
		return 0, nil
	}

	wantedServices := make(map[string]struct{}, len(services))
	for _, service := range services {
		service = strings.TrimSpace(service)
		if service == "" {
			continue
		}
		wantedServices[service] = struct{}{}
	}
	if len(wantedServices) == 0 {
		return 0, nil
	}

	projects, err := s.ListProjects()
	if err != nil {
		return 0, err
	}

	deleted := 0
	for _, project := range projects {
		for service := range wantedServices {
			if !strings.HasSuffix(project, "--"+service) {
				continue
			}
			if err := s.Delete(project); err != nil {
				return deleted, err
			}
			deleted++
			break
		}
	}

	return deleted, nil
}

func (s *Store) ListProjects() ([]string, error) {
	entries, err := os.ReadDir(s.baseDir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	projects := make([]string, 0, len(entries))
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		name := entry.Name()
		if !strings.HasSuffix(name, ".json") {
			continue
		}
		projects = append(projects, strings.TrimSuffix(name, ".json"))
	}
	sort.Strings(projects)
	return projects, nil
}
