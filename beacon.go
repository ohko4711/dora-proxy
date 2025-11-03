package main

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"strings"
)

// resolveHeadRoot queries the consensus REST API to resolve the head beacon block root.
func resolveHeadRoot(ctx context.Context, client *http.Client, consensusAPI string) (string, error) {
	base := strings.TrimRight(consensusAPI, "/")
	url := base + "/eth/v1/beacon/headers/head"

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return "", err
	}
	req.Header.Set("Accept", "application/json")

	resp, err := client.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		// try v2 blocks endpoint as a fallback
		return resolveHeadRootFallback(ctx, client, base)
	}

	var payload struct {
		Data struct {
			Root string `json:"root"`
		} `json:"data"`
	}
	dec := json.NewDecoder(resp.Body)
	if err := dec.Decode(&payload); err != nil {
		return "", err
	}

	if payload.Data.Root == "" {
		return resolveHeadRootFallback(ctx, client, base)
	}
	return payload.Data.Root, nil
}

func resolveHeadRootFallback(ctx context.Context, client *http.Client, base string) (string, error) {
	url := base + "/eth/v2/beacon/blocks/head"
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return "", err
	}
	req.Header.Set("Accept", "application/json")
	resp, err := client.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	// best-effort parse: check top-level root, or data.root
	var m map[string]interface{}
	dec := json.NewDecoder(resp.Body)
	if err := dec.Decode(&m); err != nil {
		return "", err
	}
	if v, ok := m["root"].(string); ok && v != "" {
		return v, nil
	}
	if data, ok := m["data"].(map[string]interface{}); ok {
		if v, ok := data["root"].(string); ok && v != "" {
			return v, nil
		}
	}
	return "", io.EOF
}
