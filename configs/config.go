package configs

import (
	"encoding/json"
	"os"

	"github.com/go-playground/validator/v10"
)

type ConfigData struct {
	BaseURLs       []string `json:"base_urls" validate:"required,min=1"`
	WorkersCount   int      `json:"worker_count" validate:"required,min=50,max=2000"`
	TasksCount     int      `json:"task_count" validate:"required,min=100,max=100000"`
	OnlySameDomain bool     `json:"only_same_domain" validate:"required"`
	MaxLinksInPage int      `json:"max_links_in_page" validate:"required,min=1,max=100"`
	MaxDepth       int      `json:"max_depth_crawl" validate:"required,min=1,max=10"`
	Rate           int      `json:"rate" validate:"required,min=1,max=1000"`
}

func (cfg *ConfigData) Validate() error {
	return validator.New().Struct(cfg)
}

func UploadLocalConfiguration(fileName string) (*ConfigData, error) {
	file, err := os.Open(fileName)
	if err != nil {
		return nil, err
	}

	var cfg ConfigData
	if err := json.NewDecoder(file).Decode(&cfg); err != nil {
		return nil, err
	}

	return &cfg, err
}