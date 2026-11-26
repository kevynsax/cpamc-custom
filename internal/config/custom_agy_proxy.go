package config

// CUSTOM (cpamc-custom)
type AgyProxyConfig struct {
	Listen   string `yaml:"listen" json:"listen"`
	Cert     string `yaml:"cert" json:"cert"`
	Key      string `yaml:"key" json:"key"`
	StateDir string `yaml:"state-dir" json:"state-dir"`
}
