package traefik

import (
	"os"
	"strings"

	"github.com/ku9nov/docker-compose-ztd-plugin/internal/configio"
)

func UpdateContainerIDsInConfig(path string, oldIDs []string, newIDs []string) error {
	data, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	content := string(data)

	max := len(oldIDs)
	if len(newIDs) < max {
		max = len(newIDs)
	}

	for i := 0; i < max; i++ {
		oldShort := shortID(oldIDs[i])
		newShort := shortID(newIDs[i])
		content = strings.ReplaceAll(content, oldShort, newShort)
	}

	return configio.WriteAtomic(path, []byte(content), 0o644)
}

func shortID(id string) string {
	id = strings.TrimSpace(id)
	if len(id) > 12 {
		return id[:12]
	}
	return id
}
