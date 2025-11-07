package main

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"strconv"
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

// enrichSlotConsensus fetches the beacon block from the consensus REST API and fills
// missing execution/eth1 fields in the provided slot data map.
func enrichSlotConsensus(ctx context.Context, client *http.Client, consensusAPI string, blockID string, slotData map[string]interface{}) {
	base := strings.TrimRight(consensusAPI, "/")
	url := base + "/eth/v2/beacon/blocks/" + blockID

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return
	}
	req.Header.Set("Accept", "application/json")

	resp, err := client.Do(req)
	if err != nil {
		return
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return
	}

	var payload map[string]interface{}
	dec := json.NewDecoder(resp.Body)
	if err := dec.Decode(&payload); err != nil {
		return
	}

	data, _ := payload["data"].(map[string]interface{})
	if data == nil {
		return
	}
	message, _ := data["message"].(map[string]interface{})
	if message == nil {
		return
	}
	body, _ := message["body"].(map[string]interface{})
	if body == nil {
		return
	}

	// Add dora missing fields: signature
	if sig, ok := data["signature"].(string); ok && sig != "" {
		setStringIfEmpty(slotData, "signature", sig)
	}

	// Eth1 data
	if eth1, ok := body["eth1_data"].(map[string]interface{}); ok {
		if v, ok := eth1["deposit_count"]; ok {
			if n, ok2 := parseUint64FromInterface(v); ok2 {
				setUintIfZero(slotData, "eth1data_depositcount", n)
			}
		}
		if v, ok := eth1["deposit_root"].(string); ok {
			setStringIfEmpty(slotData, "eth1data_depositroot", v)
		}
		if v, ok := eth1["block_hash"].(string); ok {
			setStringIfEmpty(slotData, "eth1data_blockhash", v)
		}
	}

	// Sync aggregate (body.sync_aggregate)
	if sa, ok := body["sync_aggregate"].(map[string]interface{}); ok {
		if v, ok := sa["sync_committee_bits"].(string); ok {
			setStringIfEmpty(slotData, "syncaggregate_bits", v)
		}
		if v, ok := sa["sync_committee_signature"].(string); ok {
			setStringIfEmpty(slotData, "syncaggregate_signature", v)
		}
	}

	// Randao reveal (body.randao_reveal)
	if rr, ok := body["randao_reveal"].(string); ok {
		setStringIfEmpty(slotData, "randaoreveal", rr)
	}

	// Execution payload(exec_logs_bloom, exec_parent_hash, exec_random, exec_receipts_root, exec_state_root, exec_timestamp)
	if exec, ok := body["execution_payload"].(map[string]interface{}); ok {
		if v, ok := exec["logs_bloom"].(string); ok {
			setStringIfEmpty(slotData, "exec_logs_bloom", v)
		}
		if v, ok := exec["parent_hash"].(string); ok {
			setStringIfEmpty(slotData, "exec_parent_hash", v)
		}
		if v, ok := exec["prev_randao"].(string); ok {
			setStringIfEmpty(slotData, "exec_random", v)
		}
		if v, ok := exec["receipts_root"].(string); ok { // some impls use receipt_root
			setStringIfEmpty(slotData, "exec_receipts_root", v)
		}
		if v, ok := exec["state_root"].(string); ok {
			setStringIfEmpty(slotData, "exec_state_root", v)
		}
		if v, ok := exec["timestamp"].(string); ok {
			setStringIfEmpty(slotData, "exec_timestamp", v)
		}

	}
}

func parseUint64FromInterface(v interface{}) (uint64, bool) {
	switch t := v.(type) {
	case string:
		if t == "" {
			return 0, false
		}
		n, err := strconv.ParseUint(t, 10, 64)
		if err != nil {
			return 0, false
		}
		return n, true
	case float64:
		return uint64(t), true
	default:
		return 0, false
	}
}

func setStringIfEmpty(m map[string]interface{}, key string, value string) {
	if m == nil || value == "" {
		return
	}
	if cur, ok := m[key]; ok {
		if s, ok := cur.(string); ok && s != "" {
			return
		}
	}
	m[key] = value
}

func setUintIfZero(m map[string]interface{}, key string, value uint64) {
	if m == nil || value == 0 {
		return
	}
	if cur, ok := m[key]; ok {
		if f, ok := cur.(float64); ok && f != 0 {
			return
		}
	}
	m[key] = value
}
