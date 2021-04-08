package storage

import (
	"github.com/pkg/errors"
	"gopkg.in/yaml.v2"
	"io/ioutil"
	"os"
	"time"
)

type Data struct {
	ChatID      int64         `yaml:"chat_id"`
	WordsetID   int           `yaml:"wordset_id"`
	Interval    time.Duration `yaml:"interval"`
	Random      bool          `yaml:"random"`
	WordsetName string        `yaml:"wordset_name"`
}

type Storage interface {
	GetData() (*Data, error)
	WriteData(data *Data) error
}

type yamlStorage struct {
	filePath string
}

func NewYamlStorage(filePath string) Storage {
	return &yamlStorage{
		filePath: filePath,
	}
}

func (s *yamlStorage) GetData() (*Data, error) {
	file, err := os.OpenFile(s.filePath, os.O_CREATE|os.O_RDONLY, 0644)
	if err != nil {
		return nil, errors.WithStack(err)
	}
	defer file.Close()
	rawData, err := ioutil.ReadAll(file)
	if err != nil {
		return nil, errors.WithStack(err)
	}
	if len(rawData) == 0 {
		return &Data{}, nil
	}
	data := Data{}
	err = yaml.Unmarshal(rawData, &data)
	if err != nil {
		return nil, errors.WithStack(err)
	}
	return &data, nil
}

func (s *yamlStorage) WriteData(data *Data) error {
	yamlData, err := yaml.Marshal(data)
	if err != nil {
		return errors.WithStack(err)
	}
	err = ioutil.WriteFile(s.filePath, yamlData, 0644)
	if err != nil {
		return errors.WithStack(err)
	}
	return nil
}
