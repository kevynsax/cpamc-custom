package config

import (
	"os"
	"path/filepath"
	"testing"

	"gopkg.in/yaml.v3"
)

func TestUsageStatisticsDefaultEnabledAndExplicitOptOut(t *testing.T) {
	for _, test := range []struct {
		name, source string
		want         bool
	}{
		{"legacy_omitted", "port: 8317\n", true},
		{"v8_omitted", "config-version: 8\nserver: {port: 8317}\n", true},
		{"legacy_false", "port: 8317\nusage-statistics-enabled: false\n", false},
		{"v8_false", "config-version: 8\nobservability:\n  usage:\n    usage-statistics-enabled: false\n", false},
		{"legacy_true", "usage-statistics-enabled: true\n", true},
		{"v8_true", "observability: {usage: {usage-statistics-enabled: true}}\n", true},
		{"v8_false_wins", "usage-statistics-enabled: true\nobservability: {usage: {usage-statistics-enabled: false}}\n", false},
		{"v8_true_wins", "usage-statistics-enabled: false\nobservability: {usage: {usage-statistics-enabled: true}}\n", true},
	} {
		t.Run(test.name, func(t *testing.T) {
			parsed, err := ParseConfigBytes([]byte(test.source))
			if err != nil {
				t.Fatal(err)
			}
			if parsed.UsageStatisticsEnabled != test.want {
				t.Fatalf("parse usage=%v want=%v", parsed.UsageStatisticsEnabled, test.want)
			}
			file := filepath.Join(t.TempDir(), "config.yaml")
			if errWrite := os.WriteFile(file, []byte(test.source), 0600); errWrite != nil {
				t.Fatal(errWrite)
			}
			loaded, errLoad := LoadConfig(file)
			if errLoad != nil {
				t.Fatal(errLoad)
			}
			if loaded.UsageStatisticsEnabled != test.want {
				t.Fatalf("load usage=%v want=%v", loaded.UsageStatisticsEnabled, test.want)
			}
		})
	}
}

func TestUsageStatisticsFalseSurvivesSaveWhenNewField(t *testing.T) {
	for _, test := range []struct{ name, source string }{
		{"legacy", "port: 8317\n"},
		{"v8", "config-version: 8\nserver: {port: 8317}\n"},
	} {
		t.Run(test.name, func(t *testing.T) {
			file := filepath.Join(t.TempDir(), "config.yaml")
			if err := os.WriteFile(file, []byte(test.source), 0600); err != nil {
				t.Fatal(err)
			}
			cfg, err := LoadConfig(file)
			if err != nil {
				t.Fatal(err)
			}
			cfg.UsageStatisticsEnabled = false
			if errSave := SaveConfigPreserveComments(file, cfg); errSave != nil {
				t.Fatal(errSave)
			}
			loaded, errLoad := LoadConfig(file)
			if errLoad != nil {
				t.Fatal(errLoad)
			}
			if loaded.UsageStatisticsEnabled {
				t.Fatal("explicit false pruned while saving")
			}
			if errSave := SaveConfigPreserveComments(file, loaded, true); errSave != nil {
				t.Fatal(errSave)
			}
			migrated, errMigrate := LoadConfig(file)
			if errMigrate != nil {
				t.Fatal(errMigrate)
			}
			if migrated.UsageStatisticsEnabled {
				t.Fatal("explicit false lost during v8 migration")
			}
		})
	}
}

func TestUsageOptOutIsNotPrunedWithOtherwiseEmptyObservability(t *testing.T) {
	var doc yaml.Node
	if err := yaml.Unmarshal([]byte("observability:\n  usage:\n    usage-statistics-enabled: false\n"), &doc); err != nil {
		t.Fatal(err)
	}
	pruneKnownDefaultsInNewNode(nil, doc.Content[0])
	value := yamlPath(doc.Content[0], "observability.usage.usage-statistics-enabled")
	if value == nil || value.Value != "false" {
		t.Fatal("opt-out removed with zero-valued parent")
	}
}

func TestExampleUsageCollectionEnabled(t *testing.T) {
	data, err := os.ReadFile(filepath.Join("..", "..", "config.example.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	cfg, errParse := ParseConfigBytes(data)
	if errParse != nil {
		t.Fatal(errParse)
	}
	if !cfg.UsageStatisticsEnabled {
		t.Fatal("example disables collection by default")
	}
}
