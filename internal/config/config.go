package config

import (
	"fmt"
	"os"
	"path/filepath"

	"gopkg.in/yaml.v3"
)

type Config struct {
	ServerURL       string   `yaml:"server_url"`
	AgentID         string   `yaml:"agent_id"`
	HostID          string   `yaml:"host_id"`
	APIKey          string   `yaml:"api_key"`
	IntervalS       int      `yaml:"interval_s"`
	WatchedServices []string `yaml:"watched_services"`
}

func DefaultConfig() *Config {
	return &Config{
		ServerURL:       "http://localhost/api",
		IntervalS:       30,
		WatchedServices: []string{},
	}
}

func LoadConfig(path string) (*Config, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return DefaultConfig(), nil
		}
		return nil, fmt.Errorf("error leyendo archivo de config: %w", err)
	}

	cfg := DefaultConfig()
	if err := yaml.Unmarshal(data, cfg); err != nil {
		return nil, fmt.Errorf("error parseando yaml de config: %w", err)
	}

	return cfg, nil
}

func SaveConfig(path string, cfg *Config) error {
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return fmt.Errorf("error creando directorio de config: %w", err)
	}

	data, err := yaml.Marshal(cfg)
	if err != nil {
		return fmt.Errorf("error serializando config a yaml: %w", err)
	}

	if err := os.WriteFile(path, data, 0600); err != nil {
		return fmt.Errorf("error escribiendo archivo de config: %w", err)
	}

	return nil
}
