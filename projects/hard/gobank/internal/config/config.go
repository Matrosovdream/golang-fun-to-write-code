package config

import (
	"fmt"
	"strings"
	"time"

	"github.com/spf13/viper"
)

type Config struct {
	Addr      string        `mapstructure:"addr"`
	DBDSN     string        `mapstructure:"db_dsn"`
	JWTSecret string        `mapstructure:"jwt_secret"`
	TokenTTL  time.Duration `mapstructure:"token_ttl"`
	LogDev    bool          `mapstructure:"log_dev"`
}

// Load reads config.yaml (if present) and lets GOBANK_* env vars override
// any key: GOBANK_DB_DSN beats db_dsn from the file.
func Load(path string) (Config, error) {
	v := viper.New()
	v.SetDefault("addr", ":8085")
	v.SetDefault("db_dsn", "postgres://gobank:gobank@localhost:5435/gobank?sslmode=disable")
	v.SetDefault("jwt_secret", "dev-secret-change-me")
	v.SetDefault("token_ttl", "24h")
	v.SetDefault("log_dev", true)

	v.SetConfigFile(path)
	if err := v.ReadInConfig(); err != nil {
		// Missing file is fine (defaults + env); a broken file is not.
		if _, ok := err.(*viper.ConfigFileNotFoundError); !ok && !strings.Contains(err.Error(), "no such file") {
			return Config{}, fmt.Errorf("read config: %w", err)
		}
	}

	v.SetEnvPrefix("gobank")
	v.SetEnvKeyReplacer(strings.NewReplacer(".", "_"))
	v.AutomaticEnv()

	var cfg Config
	if err := v.Unmarshal(&cfg); err != nil {
		return Config{}, fmt.Errorf("parse config: %w", err)
	}
	return cfg, nil
}
