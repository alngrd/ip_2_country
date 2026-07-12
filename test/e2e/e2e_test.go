//go:build e2e

// Run with: go test -tags e2e -v ./test/e2e/
// Excluded from: go test ./...  (no -tags e2e)
//
// These tests start a real HTTP server backed by the actual CSV database file.
// They catch integration bugs that unit tests with mocks cannot: CSV parsing,
// CIDR resolution, HTTP routing, and rate-limiting all exercised end-to-end.

package e2e_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync"
	"sync/atomic"
	"testing"

	"ip2country/config"
	"ip2country/database"
	"ip2country/handlers"
	"ip2country/ratelimit"
	"ip2country/server"
)

// csvPath is relative to this file's directory (test/e2e/), resolved by go test.
const csvPath = "../../data/ip2country.csv"

// =============================================================================
// Infrastructure helpers
// =============================================================================

// newServer spins up a full server using the real CSV database.
// All resources are cleaned up automatically via t.Cleanup.
func newServer(t *testing.T, rps int) *httptest.Server {
	t.Helper()

	factory := &database.Factory{}
	db, err := factory.NewDatabase("csv", csvPath)
	if err != nil {
		t.Fatalf("[server] failed to load %s: %v", csvPath, err)
	}
	rl := ratelimit.NewRateLimiter(rps)
	h := handlers.NewHandler(db, rl)
	cfg := &config.Config{Port: "0", RateLimitRPS: rps, DatabaseType: "csv", DatabasePath: csvPath}
	srv := httptest.NewServer(server.SetupServer(cfg, h).Handler)

	t.Cleanup(func() {
		srv.Close()
		rl.Stop()
		db.Close()
	})
	return srv
}

// query sends GET /v1/find-country?ip=<ip> and returns the status code and
// decoded JSON body. HTTP exchange details are omitted from normal output;
// assertion helpers below log [OK]/[FAIL] instead.
func query(t *testing.T, srv *httptest.Server, ip string) (int, map[string]string) {
	t.Helper()
	resp, err := http.Get(srv.URL + "/v1/find-country?ip=" + ip)
	if err != nil {
		t.Fatalf("GET /v1/find-country?ip=%s failed: %v", ip, err)
	}
	defer resp.Body.Close()
	var body map[string]string
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		t.Fatalf("failed to decode JSON body: %v", err)
	}
	return resp.StatusCode, body
}

// doRequest sends an arbitrary HTTP request and returns the response.
// Caller is responsible for closing resp.Body.
func doRequest(t *testing.T, method, url string) *http.Response {
	t.Helper()
	req, err := http.NewRequest(method, url, nil)
	if err != nil {
		t.Fatalf("failed to build %s %s: %v", method, url, err)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("%s %s failed: %v", method, url, err)
	}
	return resp
}

// =============================================================================
// Assertion helpers — each logs [OK] on success or [FAIL] on failure.
// Using t.Errorf for [FAIL] marks the test as failed while keeping the same
// file:line log format, so [OK] and [FAIL] lines scan identically in output.
// =============================================================================

func checkStatus(t *testing.T, expected, actual int) {
	t.Helper()
	if actual != expected {
		t.Errorf("[FAIL] status: expected %d, got %d", expected, actual)
	}
}

func checkField(t *testing.T, field, expected, actual string) {
	t.Helper()
	if actual != expected {
		t.Errorf("[FAIL] %-8s: expected %q, got %q", field, expected, actual)
	}
}

func checkNonEmpty(t *testing.T, field, actual string) {
	t.Helper()
	if actual == "" {
		t.Errorf("[FAIL] %-8s: expected non-empty string, got empty", field)
	}
}

func checkKeyCount(t *testing.T, expected int, body map[string]string) {
	t.Helper()
	if len(body) != expected {
		t.Errorf("[FAIL] key count: expected %d, got %d  body=%v", expected, len(body), body)
	}
}

func checkHeader(t *testing.T, header, expected, actual string) {
	t.Helper()
	if actual != expected {
		t.Errorf("[FAIL] %-14s: expected %q, got %q", header, expected, actual)
	}
}

// =============================================================================
// Happy-path lookups
// =============================================================================

func TestE2E_KnownIPv4ExactLookups(t *testing.T) {
	srv := newServer(t, 1000)

	tests := []struct{ ip, city, country string }{
		{"1.1.1.1", "Sydney", "Australia"},
		{"8.8.8.8", "Mountain View", "United States"},
		{"2.22.233.255", "Paris", "France"},
	}

	for _, tc := range tests {
		t.Run(tc.ip, func(t *testing.T) {
			code, body := query(t, srv, tc.ip)
			checkStatus(t, http.StatusOK, code)
			checkField(t, "city", tc.city, body["city"])
			checkField(t, "country", tc.country, body["country"])
		})
	}
}

func TestE2E_KnownIPv4CIDRLookups(t *testing.T) {
	srv := newServer(t, 1000)

	tests := []struct{ ip, cidr, city, country string }{
		{"192.168.1.50", "192.168.1.0/24", "Local Network", "Unknown"},
		{"192.168.1.1", "192.168.1.0/24", "Local Network", "Unknown"},
		{"10.0.0.1", "10.0.0.0/8", "Private Network", "Unknown"},
		{"10.255.255.254", "10.0.0.0/8", "Private Network", "Unknown"},
		{"172.16.0.1", "172.16.0.0/12", "Private Network", "Unknown"},
	}

	for _, tc := range tests {
		t.Run(tc.ip, func(t *testing.T) {
			code, body := query(t, srv, tc.ip)
			checkStatus(t, http.StatusOK, code)
			checkField(t, "city", tc.city, body["city"])
			checkField(t, "country", tc.country, body["country"])
		})
	}
}

func TestE2E_KnownIPv6ExactLookups(t *testing.T) {
	srv := newServer(t, 1000)

	tests := []struct{ ip, city, country string }{
		{"2001:4860:4860::8888", "Mountain View", "United States"},
		{"2001:4860:4860::8844", "Mountain View", "United States"},
		{"2606:4700:4700::1111", "San Francisco", "United States"},
		{"2a00:1450:4001:801::200e", "Brussels", "Belgium"},
	}

	for _, tc := range tests {
		t.Run(tc.ip, func(t *testing.T) {
			code, body := query(t, srv, tc.ip)
			checkStatus(t, http.StatusOK, code)
			checkField(t, "city", tc.city, body["city"])
			checkField(t, "country", tc.country, body["country"])
		})
	}
}

func TestE2E_KnownIPv6CIDRLookup(t *testing.T) {
	srv := newServer(t, 1000)
	code, body := query(t, srv, "2001:db8::1")
	checkStatus(t, http.StatusOK, code)
	checkField(t, "city", "Documentation Network", body["city"])
	checkField(t, "country", "Unknown", body["country"])
}

// =============================================================================
// Error cases
// =============================================================================

func TestE2E_IPNotInDatabase(t *testing.T) {
	srv := newServer(t, 1000)
	code, body := query(t, srv, "5.5.5.5")
	checkStatus(t, http.StatusNotFound, code)
	checkNonEmpty(t, "error", body["error"])
}

func TestE2E_InvalidIPFormat(t *testing.T) {
	srv := newServer(t, 1000)

	cases := []string{"not-an-ip", "999.999.999.999", "1.2.3", "abc::xyz"}
	for _, ip := range cases {
		t.Run(ip, func(t *testing.T) {
			code, body := query(t, srv, ip)
			checkStatus(t, http.StatusBadRequest, code)
			checkNonEmpty(t, "error", body["error"])
		})
	}
}

func TestE2E_MissingIPParam(t *testing.T) {
	srv := newServer(t, 1000)
	resp := doRequest(t, http.MethodGet, srv.URL+"/v1/find-country")
	resp.Body.Close()
	checkStatus(t, http.StatusBadRequest, resp.StatusCode)
}

func TestE2E_MethodNotAllowed(t *testing.T) {
	srv := newServer(t, 1000)

	for _, method := range []string{http.MethodPost, http.MethodPut, http.MethodDelete} {
		t.Run(method, func(t *testing.T) {
			resp := doRequest(t, method, srv.URL+"/v1/find-country?ip=1.1.1.1")
			resp.Body.Close()
			checkStatus(t, http.StatusMethodNotAllowed, resp.StatusCode)
		})
	}
}

func TestE2E_UnknownPathReturns404(t *testing.T) {
	srv := newServer(t, 1000)

	paths := []string{"/", "/v1", "/v2/find-country", "/healthz"}
	for _, path := range paths {
		t.Run(path, func(t *testing.T) {
			resp := doRequest(t, http.MethodGet, srv.URL+path)
			resp.Body.Close()
			checkStatus(t, http.StatusNotFound, resp.StatusCode)
		})
	}
}

// =============================================================================
// Rate limiting
// =============================================================================

func TestE2E_RateLimitBlocksExcessRequests(t *testing.T) {
	const limit = 2
	srv := newServer(t, limit)

	for i := 1; i <= limit+1; i++ {
		code, _ := query(t, srv, "1.1.1.1")
		if i <= limit && code != http.StatusOK {
			t.Errorf("[FAIL] request %d: expected 200, got %d", i, code)
		}
		if i > limit && code != http.StatusTooManyRequests {
			t.Errorf("[FAIL] request %d: expected 429, got %d", i, code)
		}
	}
}

func TestE2E_RateLimitErrorBodyContainsMessage(t *testing.T) {
	srv := newServer(t, 1)
	query(t, srv, "1.1.1.1") // consume the 1 allowed request
	code, body := query(t, srv, "1.1.1.1")
	checkStatus(t, http.StatusTooManyRequests, code)
	checkNonEmpty(t, "error", body["error"])
}

func TestE2E_RateLimitIsPerClientIP(t *testing.T) {
	// All requests originate from 127.0.0.1 (httptest), so after the first
	// succeeds the rest hit the limit regardless of the queried ?ip= value.
	srv := newServer(t, 1)
	ips := []string{"1.1.1.1", "8.8.8.8", "2.22.233.255"}
	for i, ip := range ips {
		code, _ := query(t, srv, ip)
		if i == 0 && code != http.StatusOK {
			t.Errorf("[FAIL] request 1 (?ip=%s): expected 200, got %d", ip, code)
		}
		if i > 0 && code != http.StatusTooManyRequests {
			t.Errorf("[FAIL] request %d (?ip=%s): expected 429, got %d", i+1, ip, code)
		}
	}
}

// =============================================================================
// Response contract
// =============================================================================

func TestE2E_ResponseShape(t *testing.T) {
	srv := newServer(t, 1000)
	code, body := query(t, srv, "1.1.1.1")
	checkStatus(t, http.StatusOK, code)
	checkKeyCount(t, 2, body)
	checkNonEmpty(t, "city", body["city"])
	checkNonEmpty(t, "country", body["country"])
	if _, hasErr := body["error"]; hasErr {
		t.Errorf("[FAIL] success response must not contain \"error\" key")
	}
}

func TestE2E_ErrorResponseShape(t *testing.T) {
	srv := newServer(t, 1000)

	cases := []struct {
		name string
		url  string
	}{
		{"not found", srv.URL + "/v1/find-country?ip=5.5.5.5"},
		{"bad request", srv.URL + "/v1/find-country?ip=invalid"},
		{"missing param", srv.URL + "/v1/find-country"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			resp := doRequest(t, http.MethodGet, tc.url)
			defer resp.Body.Close()
			var body map[string]string
			if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
				t.Fatalf("failed to decode body: %v", err)
			}
			checkNonEmpty(t, "error", body["error"])
			checkKeyCount(t, 1, body)
		})
	}
}

func TestE2E_ContentTypeIsJSON(t *testing.T) {
	srv := newServer(t, 1000)

	cases := []struct {
		name string
		url  string
	}{
		{"200 hit", srv.URL + "/v1/find-country?ip=1.1.1.1"},
		{"404 not found", srv.URL + "/v1/find-country?ip=5.5.5.5"},
		{"400 bad ip", srv.URL + "/v1/find-country?ip=bad"},
		{"400 missing param", srv.URL + "/v1/find-country"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			resp := doRequest(t, http.MethodGet, tc.url)
			resp.Body.Close()
			checkHeader(t, "Content-Type", "application/json", resp.Header.Get("Content-Type"))
		})
	}
}

// =============================================================================
// Concurrency
// =============================================================================

func TestE2E_ConcurrentRequestsNoRaces(t *testing.T) {
	const clients = 50
	srv := newServer(t, 10000)

	var wg sync.WaitGroup
	var successCount, failCount atomic.Int32

	for i := 0; i < clients; i++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			// Use a silent HTTP call — logging 50 interleaved requests adds no signal.
			resp, err := http.Get(srv.URL + "/v1/find-country?ip=1.1.1.1")
			if err != nil {
				failCount.Add(1)
				t.Errorf("  [FAIL] goroutine %d: request error: %v", id, err)
				return
			}
			resp.Body.Close()
			if resp.StatusCode == http.StatusOK {
				successCount.Add(1)
			} else {
				failCount.Add(1)
				t.Errorf("  [FAIL] goroutine %d: expected 200, got %d", id, resp.StatusCode)
			}
		}(i)
	}
	wg.Wait()

	if int(successCount.Load()) != clients {
		t.Errorf("[FAIL] only %d/%d requests succeeded (%d failed)",
			successCount.Load(), clients, failCount.Load())
	}
}
