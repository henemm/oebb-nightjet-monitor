package main

import (
	"fmt"
	"os"
	"time"

	"gopkg.in/yaml.v3"
)

type ConnectionConfig struct {
	From  string   `yaml:"from"`
	To    string   `yaml:"to"`
	Dates []string `yaml:"dates"`
}

type Config struct {
	SignalPhone   string             `yaml:"signal_phone"`
	SignalAPIKey  string             `yaml:"signal_apikey"`
	CheckInterval time.Duration     `yaml:"check_interval"`
	HeartbeatURL  string            `yaml:"heartbeat_url"`
	Connections   []ConnectionConfig `yaml:"connections"`
}

func (c *Config) UnmarshalYAML(node *yaml.Node) error {
	type raw struct {
		SignalPhone   string             `yaml:"signal_phone"`
		SignalAPIKey  string             `yaml:"signal_apikey"`
		CheckInterval string            `yaml:"check_interval"`
		HeartbeatURL  string            `yaml:"heartbeat_url"`
		Connections   []ConnectionConfig `yaml:"connections"`
	}
	var r raw
	if err := node.Decode(&r); err != nil {
		return err
	}

	c.SignalPhone = r.SignalPhone
	c.SignalAPIKey = r.SignalAPIKey
	c.HeartbeatURL = r.HeartbeatURL
	c.Connections = r.Connections

	dur, err := time.ParseDuration(r.CheckInterval)
	if err != nil {
		return fmt.Errorf("invalid check_interval %q: %w", r.CheckInterval, err)
	}
	c.CheckInterval = dur
	return nil
}

func LoadConfig(path string) (*Config, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("reading config: %w", err)
	}

	var cfg Config
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		return nil, fmt.Errorf("parsing config: %w", err)
	}

	if cfg.SignalPhone == "" || cfg.SignalAPIKey == "" {
		return nil, fmt.Errorf("signal_phone and signal_apikey are required")
	}
	if len(cfg.Connections) == 0 {
		return nil, fmt.Errorf("at least one connection is required")
	}
	if cfg.CheckInterval == 0 {
		cfg.CheckInterval = 60 * time.Minute
	}

	return &cfg, nil
}
