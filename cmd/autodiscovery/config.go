package main

import (
	"flag"
	"fmt"
	"os"
	"regexp"

	"github.com/goccy/go-yaml"
	"github.com/sberz/ephemeral-envs/internal/prometheus"
)

type serviceConfig struct {
	Prometheus   prometheus.Config
	StatusChecks map[string]prometheus.BaseQueryConfig
	configFile   string
	MetricsPort  int
	Port         int
}

type configFile struct {
	StatusChecks map[string]prometheus.BaseQueryConfig `yaml:"statusChecks"`
	Prometheus   prometheus.Config                     `yaml:"prometheus"`
}

var (
	nameRegex     = regexp.MustCompile(`^[-a-zA-Z0-9_]+$`)
	errInvalidKey = fmt.Errorf("key must match regex %s", nameRegex.String())
)

func (c *configFile) validate() error {
	for name, check := range c.StatusChecks {
		// Name must be a valid label value
		if !nameRegex.MatchString(name) {
			return fmt.Errorf("statusChecks.%s: %w", name, errInvalidKey)
		}

		checkErr := check.Validate()
		if checkErr != nil {
			return fmt.Errorf("statusChecks.%s: %w", name, checkErr)
		}
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

func parseConfig(args []string) (*serviceConfig, error) {
	cfg := &serviceConfig{}
	fs := flag.NewFlagSet("autodiscovery", flag.ContinueOnError)

	fs.TextVar(logLevel, "log-level", logLevel, "Set the logging level (DEBUG, INFO, WARN, ERROR)")
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
	}

	return cfg, nil
}
