package main

import (
	"net/http"
	"net/url"

	"github.com/gorilla/mux"
	"github.com/sirupsen/logrus"
)

func buildRouter(cfg *proxyConfig, client *http.Client, upstream *url.URL, log *logrus.Logger) http.Handler {
	r := mux.NewRouter()

	// POST /api/v1/validator (with status mapping)
	r.HandleFunc("/api/v1/validator", func(w http.ResponseWriter, req *http.Request) {
		if req.Method != http.MethodPost {
			w.WriteHeader(http.StatusMethodNotAllowed)
			return
		}
		proxyJSON(w, req, client, upstream, "/v1/validator", mapValidatorStatus)
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
		proxyJSON(w, req, client, upstream, path, nil)
	}).Methods(http.MethodGet)

	return r
}
