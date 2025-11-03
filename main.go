package main

import (
	"net/http"
	"net/url"
	"time"

	"github.com/sirupsen/logrus"
)

func main() {
	log := logrus.New()
	log.SetLevel(logrus.InfoLevel)

	cfg, err := loadConfig()
	if err != nil {
		log.Fatalf("failed to load config: %v", err)
	}

	upstream, err := url.Parse(cfg.UpstreamBaseURL)
	if err != nil {
		log.Fatalf("invalid PROXY_UPSTREAM_BASE_URL: %v", err)
	}

	client := &http.Client{Timeout: 20 * time.Second}

	r := buildRouter(cfg, client, upstream, log)

	srv := &http.Server{
		Addr:         cfg.ListenAddr,
		Handler:      r,
		ReadTimeout:  15 * time.Second,
		WriteTimeout: 30 * time.Second,
		IdleTimeout:  60 * time.Second,
	}

	log.Infof("dora-proxy listening on %s, upstream=%s, consensus_api=%s", cfg.ListenAddr, cfg.UpstreamBaseURL, cfg.ConsensusAPIURL)
	if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		log.Fatalf("proxy server error: %v", err)
	}
}
