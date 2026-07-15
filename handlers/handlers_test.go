package handlers_test

import (
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"testing"

	"ip2country/database"
	"ip2country/handlers"
)

// mockDB implements database.Database for testing.
type mockDB struct {
	location *database.Location
	err      error
}

func (m *mockDB) FindLocation(_ net.IP) (*database.Location, error) {
	return m.location, m.err
}

func (m *mockDB) Close() error { return nil }

func newHandler(db database.Database) *handlers.Handler {
	return handlers.NewHandler(db)
}

func get(handler *handlers.Handler, url string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(http.MethodGet, url, nil)
	req.RemoteAddr = "1.2.3.4:5678"
	rr := httptest.NewRecorder()
	handler.FindCountry(rr, req)
	return rr
}

func decodeBody(t *testing.T, rr *httptest.ResponseRecorder) map[string]string {
	t.Helper()
	var body map[string]string
	if err := json.NewDecoder(rr.Body).Decode(&body); err != nil {
		t.Fatalf("failed to decode JSON body: %v", err)
	}
	return body
}

func TestFindCountry_ValidIP(t *testing.T) {
	db := &mockDB{location: &database.Location{Country: "US", City: "New York"}}
	h := newHandler(db)
	rr := get(h, "/v1/find-country?ip=1.1.1.1")

	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rr.Code)
	}
	body := decodeBody(t, rr)
	if body["country"] != "US" || body["city"] != "New York" {
		t.Errorf("unexpected body: %v", body)
	}
	if ct := rr.Header().Get("Content-Type"); ct != "application/json" {
		t.Errorf("expected application/json content-type, got %s", ct)
	}
}

func TestFindCountry_MissingIPParam(t *testing.T) {
	h := newHandler(&mockDB{})
	rr := get(h, "/v1/find-country")

	if rr.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", rr.Code)
	}
	body := decodeBody(t, rr)
	if body["error"] == "" {
		t.Error("expected non-empty error message")
	}
}

func TestFindCountry_InvalidIPFormat(t *testing.T) {
	h := newHandler(&mockDB{})
	rr := get(h, "/v1/find-country?ip=not-an-ip")

	if rr.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", rr.Code)
	}
}

func TestFindCountry_IPNotFound(t *testing.T) {
	db := &mockDB{err: &database.NotFoundError{IP: "9.9.9.9"}}
	h := newHandler(db)
	rr := get(h, "/v1/find-country?ip=9.9.9.9")

	if rr.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d", rr.Code)
	}
}

func TestFindCountry_DatabaseError(t *testing.T) {
	db := &mockDB{err: fmt.Errorf("connection refused")}
	h := newHandler(db)
	rr := get(h, "/v1/find-country?ip=1.1.1.1")

	if rr.Code != http.StatusInternalServerError {
		t.Fatalf("expected 500, got %d", rr.Code)
	}
}

func TestFindCountry_MethodNotAllowed(t *testing.T) {
	h := newHandler(&mockDB{})
	req := httptest.NewRequest(http.MethodPost, "/v1/find-country?ip=1.1.1.1", nil)
	req.RemoteAddr = "1.2.3.4:5678"
	rr := httptest.NewRecorder()
	h.FindCountry(rr, req)

	if rr.Code != http.StatusMethodNotAllowed {
		t.Fatalf("expected 405, got %d", rr.Code)
	}
}

func TestFindCountry_IPv6Valid(t *testing.T) {
	db := &mockDB{location: &database.Location{Country: "JP", City: "Tokyo"}}
	h := newHandler(db)
	rr := get(h, "/v1/find-country?ip=2001:db8::1")

	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200 for valid IPv6, got %d", rr.Code)
	}
}
