package main

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/spf13/viper"
)

const defaultServerAddress = "server.fyra.sh:50052"

// clientConfig holds the CLI's persisted configuration.
type clientConfig struct {
	ServerAddress string `mapstructure:"server_address"`
	Token         string `mapstructure:"token"`
}

// v is the package-level viper instance, initialised by initConfig via cobra.OnInitialize.
var (
	v              *viper.Viper
	configFilePath string // resolved path used for both reads and writes
)

// initConfig is called by cobra.OnInitialize before any command runs.
func initConfig() {
	v = viper.New()
	v.SetDefault("server_address", defaultServerAddress)

	if cfgFile != "" {
		v.SetConfigFile(cfgFile)
		configFilePath = cfgFile
	} else {
		home, err := os.UserHomeDir()
		if err == nil {
			configFilePath = filepath.Join(home, ".fyra", "config.yaml")
			v.SetConfigFile(configFilePath)
		}
	}

	// DEPLOY_SERVER env overrides file; --server flag overrides env (via pflag binding).
	_ = v.BindEnv("server_address", "DEPLOY_SERVER")
	_ = v.BindPFlag("server_address", rootCmd.PersistentFlags().Lookup("server"))

	_ = v.ReadInConfig() // missing file is fine — use env/defaults

	if addr := v.GetString("server_address"); addr == "server.fyra.sh:50051" {
		v.Set("server_address", "server.fyra.sh:50052")
		_ = v.WriteConfigAs(configFilePath)
	}

}

// loadConfig returns the current config from viper.
func loadConfig() (clientConfig, error) {
	var cfg clientConfig
	if err := v.Unmarshal(&cfg); err != nil {
		return clientConfig{}, fmt.Errorf("parse config: %w", err)
	}
	return cfg, nil
}

// ensureConfig creates the config file with defaults if it does not yet exist.
func ensureConfig() error {
	if _, err := os.Stat(configFilePath); err == nil {
		return nil // already exists
	}
	cfg := clientConfig{ServerAddress: defaultServerAddress}
	return saveConfig(cfg)
}

// saveConfig writes cfg back to the config file with 0600 permissions.
func saveConfig(cfg clientConfig) error {
	v.Set("server_address", cfg.ServerAddress)
	v.Set("token", cfg.Token)

	if err := os.MkdirAll(filepath.Dir(configFilePath), 0700); err != nil {
		return fmt.Errorf("create config dir: %w", err)
	}
	if err := v.WriteConfigAs(configFilePath); err != nil {
		return fmt.Errorf("write config: %w", err)
	}
	return os.Chmod(configFilePath, 0600)
}
