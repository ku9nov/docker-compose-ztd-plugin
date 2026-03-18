package configio

import (
	"encoding/json"
	"os"
	"path/filepath"

	"gopkg.in/yaml.v3"
)

func MarshalYAML(v any) ([]byte, error) {
	return yaml.Marshal(v)
}

func UnmarshalYAML(data []byte, v any) error {
	return yaml.Unmarshal(data, v)
}

func MarshalJSON(v any) ([]byte, error) {
	return json.Marshal(v)
}

func WriteAtomic(path string, data []byte, mode os.FileMode) error {
	dir := filepath.Dir(path)
	tmp, err := os.CreateTemp(dir, "ztd-*.tmp")
	if err != nil {
		return err
	}
	defer os.Remove(tmp.Name())

	if _, err := tmp.Write(data); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Chmod(mode); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(tmp.Name(), path)
}
