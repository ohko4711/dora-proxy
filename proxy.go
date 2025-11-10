package main

import (
	"encoding/json"
	"io"
	"net/http"
	"net/url"
	"strings"
)

// proxyJSON proxies the request to upstream and optionally transforms the JSON response.
func proxyJSON(w http.ResponseWriter, req *http.Request, client *http.Client, upstream *url.URL, upstreamPath string, transform func(interface{})) {
	// Build upstream request URL
	u := *upstream
	u.Path = strings.TrimRight(upstream.Path, "/") + upstreamPath
	u.RawQuery = req.URL.RawQuery

	// Create the request
	newReq, err := http.NewRequestWithContext(req.Context(), req.Method, u.String(), req.Body)
	if err != nil {
		http.Error(w, `{"status":"ERROR: failed to create upstream request"}`, http.StatusInternalServerError)
		return
	}

	// Copy headers, prefer JSON
	copyHeaders(newReq.Header, req.Header)
	newReq.Header.Set("Accept", "application/json")

	resp, err := client.Do(newReq)
	if err != nil {
		http.Error(w, `{"status":"ERROR: upstream unreachable"}`, http.StatusBadGateway)
		return
	}
	defer resp.Body.Close()

	// Pass status and headers from upstream
	for k, vv := range resp.Header {
		for _, v := range vv {
			w.Header().Add(k, v)
		}
	}

	// Fast path: no transform, stream body through
	if transform == nil {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(resp.StatusCode)
		io.Copy(w, resp.Body)
		return
	}

	// Read the response body for transformation
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		http.Error(w, `{"status":"ERROR: failed to read upstream response"}`, http.StatusInternalServerError)
		return
	}

	// Parse JSON response
	var result interface{}
	if err := json.Unmarshal(body, &result); err != nil {
		// If not JSON, pass through as-is
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(resp.StatusCode)
		w.Write(body)
		return
	}

	// Apply transform
	transform(result)

	// Marshal back to JSON
	modifiedBody, err := json.Marshal(result)
	if err != nil {
		http.Error(w, `{"status":"ERROR: failed to marshal response"}`, http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(resp.StatusCode)
	w.Write(modifiedBody)
}

func copyHeaders(dst, src http.Header) {
	for k, vv := range src {
		// Skip hop-by-hop headers
		if shouldSkipHeader(k) {
			continue
		}
		for _, v := range vv {
			dst.Add(k, v)
		}
	}
}

func shouldSkipHeader(k string) bool {
	switch strings.ToLower(k) {
	case "accept-encoding", "connection", "keep-alive", "proxy-authenticate", "proxy-authorization", "te", "trailer", "transfer-encoding", "upgrade", "host":
		return true
	default:
		return false
	}
}

// mapValidatorStatus maps Dora status to Beacon status
// - active_ongoing -> active_online
// - withdrawal_done && slashed=true -> slashed
// - withdrawal_done && slashed=false -> exited
func mapValidatorStatus(data interface{}) {
	switch v := data.(type) {
	case map[string]interface{}:
		if status, hasStatus := v["status"].(string); hasStatus {
			slashed, _ := v["slashed"].(bool)
			switch status {
			case "active_ongoing":
				v["status"] = "active_online"
			case "withdrawal_done":
				if slashed {
					v["status"] = "slashed"
				} else {
					v["status"] = "exited"
				}
			}
		}
		for _, val := range v {
			mapValidatorStatus(val)
		}
	case []interface{}:
		for _, item := range v {
			mapValidatorStatus(item)
		}
	}
}
