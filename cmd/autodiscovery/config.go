package main

import (
	"flag"
	"fmt"
	"io"
	"log/slog"
	"os"
	"regexp"

	"github.com/goccy/go-yaml"
	"github.com/sberz/ephemeral-envs/internal/ignition"
	"github.com/sberz/ephemeral-envs/internal/probe"
	"github.com/sberz/ephemeral-envs/internal/prometheus"
)

type serviceConfig struct {
	Prometheus   prometheus.Config
	StatusChecks map[string]*prometheus.QueryConfig
	Metadata     map[string]*MetadataConfig
	Ignition     *ignition.ProviderConfig
	configFile   string
	LogLevel     slog.Level
	MetricsPort  int
	Port         int
}

type configFile struct {
	Ignition     *ignition.ProviderConfig           `yaml:"ignition"`
	StatusChecks map[string]*prometheus.QueryConfig `yaml:"statusChecks"`
	Metadata     map[string]*MetadataConfig         `yaml:"metadata"`
	Prometheus   prometheus.Config                  `yaml:"prometheus"`
}

var (
	nameRegex     = regexp.MustCompile(`^[-a-zA-Z0-9_]+$`)
	errInvalidKey = fmt.Errorf("key must match regex %s", nameRegex.String())
)

type MetadataConfig struct {
	Type                   probe.MetadataType `yaml:"type"`
	prometheus.QueryConfig `yaml:",inline"`
}

func (c *MetadataConfig) Validate() error {
	err := c.Type.Validate()
	if err != nil {
		return fmt.Errorf("invalid metadata type: %w", err)
	}

	err = c.QueryConfig.Validate()
	if err != nil {
		return fmt.Errorf("invalid query config: %w", err)
	}

	return nil
}

func (c *configFile) validate() error {

	for name, check := range c.StatusChecks {
		// Name must be a valid label value
		if !nameRegex.MatchString(name) {
			return fmt.Errorf("statusChecks.%s: %w", name, errInvalidKey)
		}

		check.Name = name
		checkErr := check.Validate()
		if checkErr != nil {
			return fmt.Errorf("statusChecks.%s: %w", name, checkErr)
		}
	}

	for name, metadata := range c.Metadata {
		if !nameRegex.MatchString(name) {
			return fmt.Errorf("metadata.%s: %w", name, errInvalidKey)
		}

		metadata.Name = name
		if err := metadata.Validate(); err != nil {
			return fmt.Errorf("metadata.%s: %w", name, err)
		}
	}

	if err := c.Ignition.Validate(); err != nil {
		return fmt.Errorf("ignition: %w", err)
	}
	return nil
}

func parseConfigFile(path string) (*configFile, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("failed to open config file: %w", err)
	}
	defer f.Close()

	cfg := &configFile{}
	decoder := yaml.NewDecoder(f, yaml.Strict())
	err = decoder.Decode(cfg)
	if err != nil {
		return nil, fmt.Errorf("failed to parse config file: %w", err)
	}

	if err := cfg.validate(); err != nil {
		return nil, fmt.Errorf("invalid config file: %w", err)
	}
	return cfg, nil
}

func parseConfig(args []string, stderr io.Writer) (*serviceConfig, error) {
	cfg := &serviceConfig{}
	fs := flag.NewFlagSet("autodiscovery", flag.ContinueOnError)
	fs.SetOutput(stderr)

	fs.TextVar(&cfg.LogLevel, "log-level", slog.LevelInfo, "Set the logging level (DEBUG, INFO, WARN, ERROR)")
	fs.IntVar(&cfg.MetricsPort, "metrics-port", 0, "Port to expose Prometheus metrics (0 to disable)")
	fs.IntVar(&cfg.Port, "port", 8080, "Port to run the HTTP server on")
	fs.StringVar(&cfg.configFile, "config", "", "Path to the configuration file")

	if err := fs.Parse(args); err != nil {
		return nil, fmt.Errorf("failed to parse args: %w", err)
	}

	if cfg.configFile != "" {
		cfgFile, err := parseConfigFile(cfg.configFile)
		if err != nil {
			return nil, fmt.Errorf("failed to parse config file: %w", err)
		}

		cfg.Prometheus = cfgFile.Prometheus
		cfg.StatusChecks = cfgFile.StatusChecks
		cfg.Metadata = cfgFile.Metadata
		cfg.Ignition = cfgFile.Ignition
	}

	return cfg, nil
}
