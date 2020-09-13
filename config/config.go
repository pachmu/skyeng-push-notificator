package config

import (
	"gopkg.in/yaml.v2"
	"io/ioutil"
	"path/filepath"
)

// Skyeng is group for skyeng client parameters.
type Skyeng struct {
	User     string `yaml:"user"`
	Password string `yaml:"password"`
}

// Pushover is group for pushover client parameters.
type Pushover struct {
	Token  string `yaml:"token"`
	User   string `yaml:"user"`
	Device string `yaml:"device"`
}

// Bot represents telegram bot parameters.
type Bot struct {
	Token string `yaml:"token"`
	User  string `yaml:"user"`
}

// Config represents parent config group.
type Config struct {
	// Port for http server.
	Port int `yaml:"http_port"`
	// Send words interval.
	SendInterval int      `yaml:"send_interval"` // seconds
	Bot          Bot      `yaml:"bot"`
	Skyeng       Skyeng   `yaml:"skyeng"`
	Pushover     Pushover `yaml:"pushover"`
}

// GetConfig returns config.
func GetConfig(cfgPath string) (*Config, error) {
	filename, err := filepath.Abs(cfgPath)
	if err != nil {
		return nil, err
	}
	yamlFile, err := ioutil.ReadFile(filename)
	if err != nil {
		return nil, err
	}
	var config Config
	if err = yaml.Unmarshal(yamlFile, &config); err != nil {
		return nil, err
	}
	return &config, nil
}
