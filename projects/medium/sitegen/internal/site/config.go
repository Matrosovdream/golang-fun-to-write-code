package site

import (
	"errors"
	"fmt"
	"io/fs"
	"strings"
)

// Config is the optional sitegen.yaml at the site root.
type Config struct {
	Title       string `yaml:"title"`
	Description string `yaml:"description"`
	BaseURL     string `yaml:"baseURL"` // absolute prefix for feed and sitemap links
}

func loadConfig(src fs.FS) (Config, error) {
	cfg := Config{Title: "My Site"}
	data, err := fs.ReadFile(src, "sitegen.yaml")
	if errors.Is(err, fs.ErrNotExist) {
		return cfg, nil
	}
	if err == nil {
		err = decodeYAML(data, &cfg)
	}
	if err != nil {
		return cfg, fmt.Errorf("sitegen.yaml: %w", err)
	}
	cfg.BaseURL = strings.TrimRight(cfg.BaseURL, "/")
	return cfg, nil
}
