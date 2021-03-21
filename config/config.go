package config

import (
	"gopkg.in/yaml.v2"
	"io/ioutil"
	"os"
	"path/filepath"
	"time"
)

// Skyeng is group for skyeng client parameters.
type Skyeng struct {
	User     string `yaml:"user"`
	Password string
}

// Pushover is group for pushover client parameters.
type Pushover struct {
	Token  string
	User   string `yaml:"user"`
	Device string `yaml:"device"`
}

// Bot represents telegram bot parameters.
type Bot struct {
	Token string
	User  string `yaml:"user"`
}

// Config represents parent config group.
type Config struct {
	// Port for http server.
	Port int `yaml:"http_port"`
	// Send words interval.
	SendInterval time.Duration `yaml:"send_interval"` // seconds
	Bot          Bot           `yaml:"bot"`
	Skyeng       Skyeng        `yaml:"skyeng"`
	Pushover     Pushover      `yaml:"pushover"`
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
	config.Skyeng.Password = os.Getenv("SKYENG_PASSWORD")
	config.Bot.Token = os.Getenv("SKYENG_BOT_TOKEN")
	return &config, nil
}
