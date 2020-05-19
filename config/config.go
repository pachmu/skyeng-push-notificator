package config

import (
	"gopkg.in/yaml.v2"
	"io/ioutil"
	"path/filepath"
)

type Skyeng struct {
	User     string `yaml:"user"`
	Password string `yaml:"password"`
}

type Pushover struct {
	Token  string `yaml:"token"`
	User   string `yaml:"user"`
	Device string `yaml:"device"`
}

type Config struct {
	Port         int      `yaml:"http_port"`
	SendInterval int      `yaml:"send_interval"` // seconds
	Skyeng       Skyeng   `yaml:"skyeng"`
	Pushover     Pushover `yaml:"pushover"`
}

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
