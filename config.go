package main

import (
	"os"
	"strings"
)

type proxyConfig struct {
	ListenAddr      string
	UpstreamBaseURL string
	ConsensusAPIURL string
}

func getEnv(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}

func loadConfig() (*proxyConfig, error) {
	cfg := &proxyConfig{
		ListenAddr:      getEnv("PROXY_LISTEN_ADDR", ":8081"),
		UpstreamBaseURL: getEnv("PROXY_UPSTREAM_BASE_URL", "http://localhost:8080"),
		ConsensusAPIURL: getEnv("PROXY_CONSENSUS_API_URL", "http://localhost:5052"),
	}

	// Ensure upstream has /api prefix once
	if !strings.HasSuffix(cfg.UpstreamBaseURL, "/api") {
		cfg.UpstreamBaseURL = strings.TrimRight(cfg.UpstreamBaseURL, "/") + "/api"
	}

	return cfg, nil
}


