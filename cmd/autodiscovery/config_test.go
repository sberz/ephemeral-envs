package main

import (
	"log/slog"
	"os"
	"path/filepath"
	"testing"
)

func TestParseConfigDefaults(t *testing.T) {
	t.Parallel()

	cfg, err := parseConfig(nil)
	if err != nil {
		t.Fatalf("parseConfig() error = %v", err)
	}

	if cfg.Port != 8080 {
		t.Fatalf("Port = %d, want %d", cfg.Port, 8080)
	}
	if cfg.MetricsPort != 0 {
		t.Fatalf("MetricsPort = %d, want %d", cfg.MetricsPort, 0)
	}
	if cfg.configFile != "" {
		t.Fatalf("configFile = %q, want empty", cfg.configFile)
	}
	if cfg.LogLevel != slog.LevelInfo {
		t.Fatalf("LogLevel = %v, want %v", cfg.LogLevel, slog.LevelInfo)
	}
}

func TestParseConfigFileLoadsChecksAndMetadata(t *testing.T) {
	t.Parallel()

	content := `prometheus:
  address: http://prometheus.example:9090
statusChecks:
  healthy:
    kind: single
    query: vector(1)
    interval: 30s
    timeout: 2s
metadata:
  owner:
    type: string
    kind: single
    query: vector(1)
    extractLabel: team
    interval: 30s
    timeout: 2s
`
	path := writeTempConfig(t, content)

	cfg, err := parseConfigFile(path)
	if err != nil {
		t.Fatalf("parseConfigFile() error = %v", err)
	}

	if cfg.Prometheus.Address != "http://prometheus.example:9090" {
		t.Fatalf("prometheus.address = %q, want %q", cfg.Prometheus.Address, "http://prometheus.example:9090")
	}

	if cfg.StatusChecks["healthy"] == nil {
		t.Fatal("statusChecks.healthy = nil, want config")
	}
	if cfg.StatusChecks["healthy"].Name != "healthy" {
		t.Fatalf("statusChecks.healthy.name = %q, want %q", cfg.StatusChecks["healthy"].Name, "healthy")
	}

	if cfg.Metadata["owner"] == nil {
		t.Fatal("metadata.owner = nil, want config")
	}
	if cfg.Metadata["owner"].Name != "owner" {
		t.Fatalf("metadata.owner.name = %q, want %q", cfg.Metadata["owner"].Name, "owner")
	}
}

func TestParseConfigFileRejectsInvalidKey(t *testing.T) {
	t.Parallel()

	content := `prometheus:
  address: http://prometheus.example:9090
statusChecks:
  bad key:
    kind: single
    query: vector(1)
    interval: 30s
    timeout: 2s
`
	path := writeTempConfig(t, content)

	if _, err := parseConfigFile(path); err == nil {
		t.Fatal("parseConfigFile() error = nil, want non-nil")
	}
}

func TestParseConfigInvalidArgs(t *testing.T) {
	t.Parallel()

	if _, err := parseConfig([]string{"--unknown-flag"}); err == nil {
		t.Fatal("parseConfig() error = nil, want non-nil")
	}
}

func TestParseConfigFileRejectsInvalidMetadataType(t *testing.T) {
	t.Parallel()

	content := `prometheus:
  address: http://prometheus.example:9090
metadata:
  owner:
    type: invalid
    kind: single
    query: vector(1)
    interval: 30s
    timeout: 2s
`
	path := writeTempConfig(t, content)

	if _, err := parseConfigFile(path); err == nil {
		t.Fatal("parseConfigFile() error = nil, want non-nil")
	}
}

func TestParseConfigLoadsConfigFileFlag(t *testing.T) {
	t.Parallel()

	content := `prometheus:
  address: http://prometheus.example:9090
statusChecks:
  healthy:
    kind: single
    query: vector(1)
    interval: 30s
    timeout: 2s
`
	path := writeTempConfig(t, content)

	cfg, err := parseConfig([]string{"--config", path, "--port", "9090", "--metrics-port", "9100", "--log-level", "DEBUG"})
	if err != nil {
		t.Fatalf("parseConfig() error = %v", err)
	}

	if cfg.Port != 9090 {
		t.Fatalf("Port = %d, want %d", cfg.Port, 9090)
	}
	if cfg.MetricsPort != 9100 {
		t.Fatalf("MetricsPort = %d, want %d", cfg.MetricsPort, 9100)
	}
	if cfg.Prometheus.Address != "http://prometheus.example:9090" {
		t.Fatalf("Prometheus.Address = %q, want %q", cfg.Prometheus.Address, "http://prometheus.example:9090")
	}
	if _, ok := cfg.StatusChecks["healthy"]; !ok {
		t.Fatalf("status checks = %#v, want key %q", cfg.StatusChecks, "healthy")
	}

	if cfg.LogLevel != slog.LevelDebug {
		t.Fatalf("LogLevel = %v, want %v", cfg.LogLevel, slog.LevelDebug)
	}
}

func TestParseConfigFileIgnition(t *testing.T) {
	t.Parallel()

	tests := map[string]struct {
		check   func(t *testing.T, cfg *configFile)
		content string
		wantErr bool
	}{
		"loads explicit prometheus ignition provider": {
			content: `ignition:
  type: prometheus
`,
			check: func(t *testing.T, cfg *configFile) {
				t.Helper()
				if cfg.Ignition == nil {
					t.Fatal("ignition = nil, want config")
				}
				if cfg.Ignition.Type != "prometheus" {
					t.Fatalf("ignition.type = %q, want %q", cfg.Ignition.Type, "prometheus")
				}
			},
		},
		"allows empty ignition config": {
			content: `ignition:
  {}
`,
			check: func(t *testing.T, cfg *configFile) {
				t.Helper()
				if cfg.Ignition == nil {
					t.Fatal("ignition = nil, want config")
				}
				if !cfg.Ignition.IsZero() {
					t.Fatalf("ignition = %#v, want zero config", cfg.Ignition)
				}
			},
		},
		"rejects invalid ignition provider type": {
			content: `ignition:
  type: unknown
`,
			wantErr: true,
		},
	}

	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			path := writeTempConfig(t, tt.content)
			cfg, err := parseConfigFile(path)
			if tt.wantErr {
				if err == nil {
					t.Fatal("parseConfigFile() error = nil, want non-nil")
				}
				return
			}

			if err != nil {
				t.Fatalf("parseConfigFile() error = %v", err)
			}

			if tt.check != nil {
				tt.check(t, cfg)
			}
		})
	}
}

func writeTempConfig(t *testing.T, content string) string {
	t.Helper()

	path := filepath.Join(t.TempDir(), "config.yaml")
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}

	return path
}
