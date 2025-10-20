package configs

import (
	"encoding/json"
	"os"
)

type ConfigData struct {
	BaseURLs       		[]string `json:"base_urls" validate:"required,len=1:20"`
	InfoLogPath   		string   `json:"info_log_path" validate:"required"` // use '-' for stdout
	ErrorLogPath  		string   `json:"error_log_path" validate:"required"` // use '-' for stderr
	LogChannelSize 		int      `json:"log_channel_size" validate:"min=1000,max=50000"`
	WorkersCount   		int      `json:"worker_count" validate:"min=50,max=2000"`
	TasksCount     		int      `json:"task_count" validate:"min=100,max=10000"`
	MaxLinksInPage 		int      `json:"max_links_in_page" validate:"min=1,max=100"`
	MaxDepth       		int      `json:"max_depth_crawl" validate:"min=1,max=10"`
	NGramCount    		int      `json:"ngram_count" validate:"min=2,max=5"`
	MaxTypo	  			int      `json:"max_typo" validate:"min=1,max=4"`
	MaxTransactionBytes int      `json:"max_transaction_bytes" validate:"min=1048576,max=15728640"`
	OnlySameDomain 		bool     `json:"only_same_domain"`
}

func (cfg *ConfigData) Validate() error {
	return New("validate").Validate(*cfg)
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