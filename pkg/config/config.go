package config

import (
	"encoding/json"
	"fmt"
	"os"
	"time"
)

type Config struct {
	ServerURL       string
	AllowHTTP       bool
	Debug           bool
	Transport       string
	Headers         map[string]string
	ConfigDir       string
	ProtocolVersion string
	Timeout         time.Duration
	OAuth           bool
	ClientID        string
	ClientSecret    string
}

type fileConfig struct {
	URL             string            `json:"url"`
	Headers         map[string]string `json:"headers"`
	Transport       string            `json:"transport"`
	Debug           bool              `json:"debug"`
	AllowHTTP       bool              `json:"allowHttp"`
	ProtocolVersion string            `json:"protocolVersion"`
	TimeoutSeconds  int               `json:"timeoutSeconds"`
	OAuth           *bool             `json:"oauth"`
	ClientID        string            `json:"clientId"`
	ClientSecret    string            `json:"clientSecret"`
}

func LoadFile(path string) (*fileConfig, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read config: %w", err)
	}
	var parsed fileConfig
	if err := json.Unmarshal(data, &parsed); err != nil {
		return nil, fmt.Errorf("parse config: %w", err)
	}
	for key, value := range parsed.Headers {
		parsed.Headers[key] = os.ExpandEnv(value)
	}
	parsed.ClientSecret = os.ExpandEnv(parsed.ClientSecret)
	parsed.ClientID = os.ExpandEnv(parsed.ClientID)
	return &parsed, nil
}

func (c *fileConfig) Apply(dst *Config) {
	if c.URL != "" {
		dst.ServerURL = c.URL
	}
	if c.Transport != "" {
		dst.Transport = c.Transport
	}
	if c.ProtocolVersion != "" {
		dst.ProtocolVersion = c.ProtocolVersion
	}
	if c.TimeoutSeconds > 0 {
		dst.Timeout = time.Duration(c.TimeoutSeconds) * time.Second
	}
	dst.Debug = c.Debug
	dst.AllowHTTP = c.AllowHTTP
	if c.OAuth != nil {
		dst.OAuth = *c.OAuth
	}
	if c.ClientID != "" {
		dst.ClientID = c.ClientID
	}
	if c.ClientSecret != "" {
		dst.ClientSecret = c.ClientSecret
	}
	if len(c.Headers) > 0 {
		if dst.Headers == nil {
			dst.Headers = make(map[string]string)
		}
		for k, v := range c.Headers {
			dst.Headers[k] = v
		}
	}
}
