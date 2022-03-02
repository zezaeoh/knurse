package config

import (
	"github.com/pkg/errors"
	"gopkg.in/yaml.v3"
	"io/ioutil"
	"os"
	"path/filepath"
)

type Config struct {
	ConfigDir string

	Webhook struct {
		ConfigName string `yaml:"configName"`
		CaCerts    struct {
			Name              string `yaml:"name"`
			Path              string `yaml:"path"`
			Data              string `yaml:"data"`
			SetupCaCertsImage string `yaml:"setupCaCertsImage"`
		} `yaml:"caCerts"`
	} `yaml:"webhook"`
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
	if cfg.Webhook.ConfigName == "" {
		return errors.New("webhook.configName: required but empty")
	}
	if cfg.Webhook.CaCerts.Name == "" {
		return errors.New("webhook.caCerts.name: required but empty")
	}
	if cfg.Webhook.CaCerts.Path == "" {
		return errors.New("webhook.caCerts.path: required but empty")
	}
	if cfg.Webhook.CaCerts.SetupCaCertsImage == "" {
		return errors.New("webhook.caCerts.setupCaCertsImage: required but empty")
	}
	return nil
}
