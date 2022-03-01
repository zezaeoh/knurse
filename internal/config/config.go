package config

import (
	"gopkg.in/yaml.v3"
	"io/ioutil"
	"os"
	"path/filepath"
)

type Config struct {
	ConfigDir         string
	SetupCaCertsImage string `yaml:"setupCaCertsImage"`
	CaCertsData       string `yaml:"caCertsData"`
}

func LoadConfig() (*Config, error) {
	return loadConfig(configPath)
}

func loadConfig(configPath string) (*Config, error) {
	configDir, err := filepath.Abs(configPath)
	if err != nil {
		return nil, err
	}

	f, err := os.Open(configDir)
	if err != nil {
		return nil, err
	}

	b, err := ioutil.ReadAll(f)
	if err != nil {
		return nil, err
	}
	// expand env vars for secrets
	b = []byte(os.ExpandEnv(string(b)))

	cfg, err := parseConfig(b)
	if err != nil {
		return nil, err
	}

	err = validateConfig(cfg)
	if err != nil {
		return nil, err
	}

	cfg.ConfigDir = configDir
	return cfg, nil
}

func parseConfig(b []byte) (*Config, error) {
	cfg := new(Config)
	if err := yaml.Unmarshal(b, cfg); err != nil {
		return nil, err
	}
	return cfg, nil
}

func validateConfig(cfg *Config) error {
	return nil
}
