package main

import (
	"net/http"
	"net/url"

	"github.com/gorilla/mux"
)

func buildRouter(cfg *proxyConfig, client *http.Client, upstream *url.URL, cache *LastAttestCache) http.Handler {
	r := mux.NewRouter()

	// POST /api/v1/validator (with status mapping)
	r.HandleFunc("/api/v1/validator", func(w http.ResponseWriter, req *http.Request) {
		if req.Method != http.MethodPost {
			w.WriteHeader(http.StatusMethodNotAllowed)
			return
		}
		transform := func(body interface{}) {
			// remap status
			mapValidatorStatus(body)
			// inject lastattestslot using cache
			attachLastAttestSlot(body, cache)
		}
		proxyJSON(w, req, client, upstream, "/v1/validator", transform)
	}).Methods(http.MethodPost)

	// GET /api/v1/epoch/latest
	r.HandleFunc("/api/v1/epoch/latest", func(w http.ResponseWriter, req *http.Request) {
		proxyJSON(w, req, client, upstream, "/v1/epoch/latest", nil)
	}).Methods(http.MethodGet)

	// GET /api/v1/slot/{slotOrHash}
	r.HandleFunc("/api/v1/slot/{slotOrHash}", func(w http.ResponseWriter, req *http.Request) {
		vars := mux.Vars(req)
		id := vars["slotOrHash"]

		if id == "head" {
			root, err := resolveHeadRoot(req.Context(), client, cfg.ConsensusAPIURL)
			if err != nil {
				http.Error(w, `{"status":"ERROR: failed to resolve head"}`, http.StatusBadGateway)
				return
			}
			id = root
		}

		path := "/v1/slot/" + id
		// Enrich and then project into Dora base fields + Beacon-missing fields
		transform := func(body interface{}) {
			root, ok := body.(map[string]interface{})
			if !ok {
				return
			}
			data, _ := root["data"].(map[string]interface{})
			if data == nil {
				return
			}
			enrichSlotConsensus(req.Context(), client, cfg.ConsensusAPIURL, id, data)
			root["data"] = buildSlotResponseFromMap(data)
		}
		proxyJSON(w, req, client, upstream, path, transform)
	}).Methods(http.MethodGet)

	return r
}
