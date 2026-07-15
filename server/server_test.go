package server_test

import (
	"net"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"ip2country/config"
	"ip2country/database"
	"ip2country/handlers"
	"ip2country/ratelimit"
	"ip2country/server"
)

type stubDB struct{ location *database.Location }

func (s *stubDB) FindLocation(_ net.IP) (*database.Location, error) {
	if s.location != nil {
		return s.location, nil
	}
	return nil, &database.NotFoundError{IP: "0.0.0.0"}
}
func (s *stubDB) Close() error { return nil }

func newTestServer(t *testing.T) *http.Server {
	t.Helper()
	rl := ratelimit.NewRateLimiter(100)
	t.Cleanup(rl.Stop)
	h := handlers.NewHandler(&stubDB{location: &database.Location{Country: "US", City: "NYC"}}, rl)
	cfg := &config.Config{Port: "0", RateLimitRPS: 100, DatabaseURL: "csv:"}
	return server.SetupServer(cfg, h)
}

func TestSetupServer_KnownRouteIsHandled(t *testing.T) {
	srv := newTestServer(t)
	req := httptest.NewRequest(http.MethodGet, "/v1/find-country?ip=1.1.1.1", nil)
	req.RemoteAddr = "127.0.0.1:9999"
	rr := httptest.NewRecorder()
	srv.Handler.ServeHTTP(rr, req)

	if rr.Code == http.StatusNotFound {
		t.Fatal("/v1/find-country should be registered; got 404 from ServeMux")
	}
	if rr.Code != http.StatusOK {
		t.Errorf("expected 200, got %d", rr.Code)
	}
}

func TestSetupServer_UnknownPathReturns404(t *testing.T) {
	srv := newTestServer(t)
	req := httptest.NewRequest(http.MethodGet, "/unknown/path", nil)
	req.RemoteAddr = "127.0.0.1:9999"
	rr := httptest.NewRecorder()
	srv.Handler.ServeHTTP(rr, req)

	if rr.Code != http.StatusNotFound {
		t.Errorf("expected 404 for unregistered path, got %d", rr.Code)
	}
}

func TestSetupServer_TimeoutsConfigured(t *testing.T) {
	srv := newTestServer(t)
	if srv.ReadTimeout == 0 {
		t.Error("ReadTimeout must be set to protect against slow clients")
	}
	if srv.WriteTimeout == 0 {
		t.Error("WriteTimeout must be set to protect against slow clients")
	}
	if srv.IdleTimeout == 0 {
		t.Error("IdleTimeout must be set for keep-alive connections")
	}
}

func TestSetupServer_PortFromConfig(t *testing.T) {
	rl := ratelimit.NewRateLimiter(10)
	t.Cleanup(rl.Stop)
	h := handlers.NewHandler(&stubDB{}, rl)
	cfg := &config.Config{Port: "9876"}
	srv := server.SetupServer(cfg, h)

	if srv.Addr != ":9876" {
		t.Errorf("expected server addr :9876, got %s", srv.Addr)
	}
}

func TestSetupServer_RootPathReturns404(t *testing.T) {
	srv := newTestServer(t)
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.RemoteAddr = "127.0.0.1:9999"
	rr := httptest.NewRecorder()
	srv.Handler.ServeHTTP(rr, req)

	if rr.Code != http.StatusNotFound {
		t.Errorf("expected 404 for root path, got %d", rr.Code)
	}
}

func TestSetupServer_ResponseTimeIsReasonable(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping timing test in short mode")
	}
	srv := newTestServer(t)
	req := httptest.NewRequest(http.MethodGet, "/v1/find-country?ip=8.8.8.8", nil)
	req.RemoteAddr = "127.0.0.1:9999"
	rr := httptest.NewRecorder()

	start := time.Now()
	srv.Handler.ServeHTTP(rr, req)
	elapsed := time.Since(start)

	if elapsed > 100*time.Millisecond {
		t.Errorf("handler took too long: %v (expected <100ms for in-memory lookup)", elapsed)
	}
}
