package tui

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"strings"

	"github.com/tailscale/hujson"
)

const (
	defaultAddrFallback  = "127.0.0.1:6999"
	defaultTokenFallback = ""
)

type runtimeOptions struct {
	addr  string
	token string
}

type flowLayerConfig struct {
	Session  flowLayerSessionConfig            `json:"session,omitempty"`
	Logs     json.RawMessage                   `json:"logs,omitempty"`
	Services map[string]flowLayerServiceConfig `json:"services,omitempty"`
}

type flowLayerSessionConfig struct {
	Addr  string `json:"addr,omitempty"`
	Token string `json:"token,omitempty"`
}

type flowLayerServiceConfig struct {
	Cmd       json.RawMessage   `json:"cmd,omitempty"`
	StopCmd   json.RawMessage   `json:"stopCmd,omitempty"`
	Kind      string            `json:"kind,omitempty"`
	Port      int               `json:"port,omitempty"`
	Ready     json.RawMessage   `json:"ready,omitempty"`
	Env       map[string]string `json:"env,omitempty"`
	DependsOn []string          `json:"dependsOn,omitempty"`
}

func resolveRuntimeOptions(configPath string, addrFlagValue string, addrFlagProvided bool, tokenFlagValue string, tokenFlagProvided bool) (runtimeOptions, error) {
	options := runtimeOptions{
		addr:  defaultAddrFallback,
		token: defaultTokenFallback,
	}

	trimmedConfigPath := strings.TrimSpace(configPath)
	if trimmedConfigPath != "" {
		cfg, err := loadFlowLayerConfig(trimmedConfigPath)
		if err != nil {
			return runtimeOptions{}, err
		}

		if addr := strings.TrimSpace(cfg.Session.Addr); addr != "" {
			options.addr = addr
		}
		options.token = strings.TrimSpace(cfg.Session.Token)
	}

	if addrFlagProvided {
		options.addr = strings.TrimSpace(addrFlagValue)
	}
	if tokenFlagProvided {
		options.token = strings.TrimSpace(tokenFlagValue)
	}

	if strings.TrimSpace(options.addr) == "" {
		return runtimeOptions{}, fmt.Errorf("resolved address is empty")
	}
	if !validateAddress(options.addr) {
		return runtimeOptions{}, fmt.Errorf("resolved address %q is invalid; expected host:port", options.addr)
	}

	return options, nil
}

func loadFlowLayerConfig(path string) (*flowLayerConfig, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read config file %q: %w", path, err)
	}

	cfg, err := parseFlowLayerConfigJSONC(raw)
	if err != nil {
		return nil, fmt.Errorf("parse config file %q: %w", path, err)
	}

	if err := validateFlowLayerConfig(cfg); err != nil {
		return nil, err
	}

	return cfg, nil
}

func parseFlowLayerConfigJSONC(raw []byte) (*flowLayerConfig, error) {
	value, err := hujson.Parse(raw)
	if err != nil {
		return nil, err
	}
	value.Standardize()
	jsonBytes := value.Pack()

	var cfg flowLayerConfig
	decoder := json.NewDecoder(bytes.NewReader(jsonBytes))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&cfg); err != nil {
		return nil, err
	}

	if cfg.Services == nil {
		cfg.Services = map[string]flowLayerServiceConfig{}
	}

	return &cfg, nil
}

func validateFlowLayerConfig(cfg *flowLayerConfig) error {
	if cfg == nil {
		return fmt.Errorf("config is nil")
	}

	return nil
}

func copyIntPointer(value *int) *int {
	if value == nil {
		return nil
	}
	copied := *value
	return &copied
}
