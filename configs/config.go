package configs

import (
	"encoding/json"
	"os"
)

type ConfigData struct {
	BaseURLs       []string `json:"base_urls" validate:"required"`
	WorkersCount   int      `json:"worker_count" validate:"required"`
	TasksCount     int      `json:"task_count" validate:"required"`
	OnlySameDomain bool     `json:"only_same_domain" validate:"required"`
	MaxLinksInPage int      `json:"max_links_in_page" validate:"required"`
	MaxDepth       int      `json:"max_depth_crawl" validate:"required"`
	Rate           int      `json:"rate" validate:"required"`
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