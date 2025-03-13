package configs

import (
	"encoding/json"
	"os"
)

type ConfigData struct {
	BaseURLs       []string `json:"base_urls"`
	WorkersCount   int      `json:"worker_count"`
	TasksCount     int      `json:"task_count"`
	OnlySameDomain bool     `json:"only_same_domain"`
	MaxLinksInPage int      `json:"max_links_in_page"`
	MaxDepth       int      `json:"max_depth_crawl"`
	Rate           int      `json:"rate"`
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