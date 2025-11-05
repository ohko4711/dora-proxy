package main

import (
	"context"
	"encoding/hex"
	"encoding/json"
	"io"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/sirupsen/logrus"
)

const (
	secondsPerSlot = 12
	slotsPerEpoch  = 32
)

type LastAttestCache struct {
	mu sync.RWMutex
	m  map[uint64]uint64 // validatorIndex -> lastAttestSlot
}

func NewLastAttestCache() *LastAttestCache {
	return &LastAttestCache{m: make(map[uint64]uint64)}
}

func (c *LastAttestCache) Get(index uint64) uint64 {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.m[index]
}

func (c *LastAttestCache) SetIfGreater(index uint64, slot uint64) bool {
	c.mu.Lock()
	updated := false
	if cur, ok := c.m[index]; !ok || slot > cur {
		c.m[index] = slot
		updated = true
	}
	c.mu.Unlock()
	return updated
}

type AttestationTracker struct {
	client       *http.Client
	consensusAPI string
	cache        *LastAttestCache
	log          logrus.FieldLogger

	mu               sync.Mutex
	lastScannedEpoch uint64
	lastScannedSlot  uint64
}

func NewAttestationTracker(client *http.Client, consensusAPI string, cache *LastAttestCache, log logrus.FieldLogger) *AttestationTracker {
	return &AttestationTracker{client: client, consensusAPI: consensusAPI, cache: cache, log: log}
}

// Start begins a background goroutine that scans the most recently completed epoch
// on a fixed schedule. It is best-effort and silent on errors.
func (t *AttestationTracker) Start() {
	go func() {
		// 每个slot扫描一次
		ticker := time.NewTicker(time.Duration(secondsPerSlot) * time.Second)
		defer ticker.Stop()
		t.log.Info("attestation slot scanner started")
		for range ticker.C {
			ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
			headSlot, err := t.getHeadSlot(ctx)
			cancel()
			if err != nil {
				t.log.WithError(err).Warn("failed to get head slot for slot scan")
				continue
			}

			t.mu.Lock()
			start := t.lastScannedSlot + 1
			if t.lastScannedSlot == 0 { // first run: only current head
				start = headSlot
			}
			already := start > headSlot
			t.mu.Unlock()
			if already {
				continue
			}

			count := (headSlot - start + 1)
			t.log.WithFields(logrus.Fields{"from": start, "to": headSlot, "count": count}).Info("scanning new slots")

			ctx2, cancel2 := context.WithTimeout(context.Background(), 90*time.Second)
			var slots uint64
			var updates uint64
			aborted := false
		slotsLoop:
			for s := start; s <= headSlot; s++ {
				select {
				case <-ctx2.Done():
					aborted = true
					break slotsLoop
				default:
				}
				slots++
				updates += t.processSlot(ctx2, s)
			}
			cancel2()

			t.mu.Lock()
			t.lastScannedSlot = headSlot
			t.mu.Unlock()

			if aborted {
				t.log.WithFields(logrus.Fields{"from": start, "to": headSlot, "slots": slots, "updates": updates}).Warn("slot scan aborted (timeout)")
			} else {
				t.log.WithFields(logrus.Fields{"from": start, "to": headSlot, "slots": slots, "updates": updates}).Info("slot scan finished")
			}
		}
	}()
}

// Backfill scans only the most recent 3 epochs starting from head,
// newest to oldest, populating the cache.
func (t *AttestationTracker) Backfill(ctx context.Context) error {
	headSlot, err := t.getHeadSlot(ctx)
	if err != nil {
		return err
	}
	headEpoch := headSlot / slotsPerEpoch
	var end uint64
	if headEpoch >= 2 {
		end = headEpoch - 2
	} else {
		end = 0
	}
	t.log.WithFields(logrus.Fields{"from": headEpoch, "to": end}).Info("backfill scanning epochs range")
	slots, updates, err := t.scanEpochRange(ctx, headEpoch, end)
	if err != nil {
		t.log.WithError(err).Warn("backfill encountered error")
		return err
	}
	t.log.WithFields(logrus.Fields{"epochs": (headEpoch - end + 1), "slots": slots, "updates": updates}).Info("backfill completed")
	return nil
}

func (t *AttestationTracker) getHeadSlot(ctx context.Context) (uint64, error) {
	base := strings.TrimRight(t.consensusAPI, "/")
	url := base + "/eth/v2/beacon/blocks/head"
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return 0, err
	}
	req.Header.Set("Accept", "application/json")
	resp, err := t.client.Do(req)
	if err != nil {
		return 0, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return 0, io.EOF
	}
	var payload struct {
		Data struct {
			Message struct {
				Slot string `json:"slot"`
			} `json:"message"`
		} `json:"data"`
	}
	dec := json.NewDecoder(resp.Body)
	if err := dec.Decode(&payload); err != nil {
		return 0, err
	}
	if payload.Data.Message.Slot == "" {
		return 0, io.EOF
	}
	n, err := strconv.ParseUint(payload.Data.Message.Slot, 10, 64)
	if err != nil {
		return 0, err
	}
	return n, nil
}

func (t *AttestationTracker) scanEpochRange(ctx context.Context, startEpoch, endEpoch uint64) (uint64, uint64, error) {
	// iterate newest to oldest, but process slots with bounded concurrency
	const maxConcurrency = 16
	jobs := make(chan uint64, maxConcurrency*2)
	var slotsScanned uint64
	var updates uint64
	var wg sync.WaitGroup

	// workers
	for i := 0; i < maxConcurrency; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for slot := range jobs {
				// stop if context expired
				select {
				case <-ctx.Done():
					return
				default:
				}
				u := t.processSlot(ctx, slot)
				atomic.AddUint64(&slotsScanned, 1)
				atomic.AddUint64(&updates, u)
			}
		}()
	}

	// producer: newest -> oldest
	produceAborted := false
	for epoch := startEpoch; ; epoch-- {
		startSlot := epoch * slotsPerEpoch
		endSlot := startSlot + (slotsPerEpoch - 1)
		for slot := endSlot; ; slot-- {
			// stop if context expired
			select {
			case <-ctx.Done():
				produceAborted = true
			default:
			}
			if produceAborted {
				break
			}
			select {
			case <-ctx.Done():
				produceAborted = true
			case jobs <- slot:
			}
			if slot == startSlot {
				break
			}
		}
		if produceAborted {
			break
		}
		if epoch == endEpoch {
			break
		}
		if epoch == 0 { // avoid underflow
			break
		}
	}
	close(jobs)
	wg.Wait()

	if err := ctx.Err(); err != nil {
		return atomic.LoadUint64(&slotsScanned), atomic.LoadUint64(&updates), err
	}
	return atomic.LoadUint64(&slotsScanned), atomic.LoadUint64(&updates), nil
}

func (t *AttestationTracker) processSlot(ctx context.Context, slot uint64) uint64 {
	base := strings.TrimRight(t.consensusAPI, "/")
	url := base + "/eth/v2/beacon/blocks/" + strconv.FormatUint(slot, 10)

	// Retry fetching the block a few times on transient failures
	const maxAttempts = 3
	var resp *http.Response
	for attempt := 1; attempt <= maxAttempts; attempt++ {
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
		if err != nil {
			t.log.WithError(err).Debug("build request for block failed")
			return 0
		}
		req.Header.Set("Accept", "application/json")

		r, err := t.client.Do(req)
		if err == nil && r != nil && r.StatusCode == http.StatusOK {
			resp = r
			break
		}

		if err != nil {
			t.log.WithFields(logrus.Fields{"slot": slot, "attempt": attempt, "max": maxAttempts}).WithError(err).Debug("fetch block failed, will retry")
		} else if r != nil {
			t.log.WithFields(logrus.Fields{"slot": slot, "status": r.StatusCode, "attempt": attempt, "max": maxAttempts}).Debug("block request non-200, will retry")
			// drain and close before retrying
			io.Copy(io.Discard, r.Body)
			r.Body.Close()
		}

		if attempt == maxAttempts {
			return 0
		}

		backoff := time.Duration(attempt*100) * time.Millisecond
		select {
		case <-ctx.Done():
			return 0
		case <-time.After(backoff):
		}
	}
	if resp == nil {
		return 0
	}
	defer resp.Body.Close()
	var payload map[string]interface{}
	if err := json.NewDecoder(resp.Body).Decode(&payload); err != nil {
		t.log.WithError(err).Debug("decode block JSON failed")
		return 0
	}
	data, _ := payload["data"].(map[string]interface{})
	if data == nil {
		return 0
	}
	message, _ := data["message"].(map[string]interface{})
	if message == nil {
		return 0
	}
	body, _ := message["body"].(map[string]interface{})
	if body == nil {
		return 0
	}
	attestations, _ := body["attestations"].([]interface{})
	if len(attestations) == 0 {
		return 0
	}
	// fetch committees for this slot once
	idxToValidators := t.fetchCommitteesForSlot(ctx, slot)
	var updated uint64
	for _, a := range attestations {
		att, _ := a.(map[string]interface{})
		if att == nil {
			continue
		}
		voters := t.validatorsForAttestation(att, idxToValidators)
		for _, vi := range voters {
			if t.cache.SetIfGreater(vi, slot) {
				updated++
			}
		}
	}
	return updated
}

func (t *AttestationTracker) fetchCommitteesForSlot(ctx context.Context, slot uint64) map[uint64][]uint64 {
	base := strings.TrimRight(t.consensusAPI, "/")
	stateID := strconv.FormatUint(slot, 10)
	url := base + "/eth/v1/beacon/states/" + stateID + "/committees?slot=" + strconv.FormatUint(slot, 10)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		t.log.WithError(err).Debug("build request for committees failed")
		return nil
	}
	req.Header.Set("Accept", "application/json")
	resp, err := t.client.Do(req)
	if err != nil {
		t.log.WithError(err).Debug("fetch committees failed")
		return nil
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.log.WithFields(logrus.Fields{"slot": slot, "status": resp.StatusCode}).Debug("committees request non-200")
		return nil
	}
	var payload struct {
		Data []struct {
			Index      string   `json:"index"`
			Validators []string `json:"validators"`
		} `json:"data"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&payload); err != nil {
		t.log.WithError(err).Debug("decode committees JSON failed")
		return nil
	}
	res := make(map[uint64][]uint64, len(payload.Data))
	for _, c := range payload.Data {
		idx, err := strconv.ParseUint(c.Index, 10, 64)
		if err != nil {
			continue
		}
		vals := make([]uint64, 0, len(c.Validators))
		for _, s := range c.Validators {
			vi, err := strconv.ParseUint(s, 10, 64)
			if err != nil {
				continue
			}
			vals = append(vals, vi)
		}
		res[idx] = vals
	}
	return res
}

func (t *AttestationTracker) validatorsForAttestation(att map[string]interface{}, idxToValidators map[uint64][]uint64) []uint64 {
	var voters []uint64
	aggBitsStr, _ := att["aggregation_bits"].(string)
	aggBits := hexBitlist(aggBitsStr)
	// Electra multi-committee path
	if cbitsStr, ok := att["committee_bits"].(string); ok && cbitsStr != "" {
		cbits := hexBitlist(cbitsStr)
		included := make([]uint64, 0, len(cbits))
		for i, b := range cbits {
			if b {
				included = append(included, uint64(i))
			}
		}
		var concat []uint64
		for _, ci := range included {
			concat = append(concat, idxToValidators[ci]...)
		}
		for i, b := range aggBits {
			if b && i < len(concat) {
				voters = append(voters, concat[i])
			}
		}
		return voters
	}
	return voters
}

func hexBitlist(hexstr string) []bool {
	if hexstr == "" {
		return nil
	}
	hs := strings.TrimPrefix(strings.ToLower(hexstr), "0x")
	b, err := hex.DecodeString(hs)
	if err != nil {
		return nil
	}
	bits := make([]bool, 0, len(b)*8)
	for _, by := range b {
		for i := 0; i < 8; i++ { // LSB-first per SSZ bitlist
			bits = append(bits, ((by>>uint(i))&1) == 1)
		}
	}
	return bits
}

// attachLastAttestSlot recursively injects lastattestslot into any object that appears
// to represent a validator (has index or validator_index field).
func attachLastAttestSlot(v interface{}, cache *LastAttestCache) {
	switch m := v.(type) {
	case map[string]interface{}:
		if val, has := m["validatorindex"]; has {
			if idx, ok := parseUint64FromInterface(val); ok {
				m["lastattestationslot"] = cache.Get(idx)
			}
		}
		// Recurse on nested objects/arrays
		for _, val := range m {
			attachLastAttestSlot(val, cache)
		}
	case []interface{}:
		for _, it := range m {
			attachLastAttestSlot(it, cache)
		}
	}
}
