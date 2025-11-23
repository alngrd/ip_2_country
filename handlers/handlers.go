package handlers

import (
	"encoding/json"
	"ip2country/database"
	"ip2country/ratelimit"
	"log"
	"net"
	"net/http"
)

type Handler struct {
	db          database.Database
	rateLimiter *ratelimit.RateLimiter
}

func NewHandler(db database.Database, rateLimiter *ratelimit.RateLimiter) *Handler {
	return &Handler{
		db:          db,
		rateLimiter: rateLimiter,
	}
}

func (h *Handler) FindCountry(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		h.writeError(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	clientIP := getClientIP(r)
	if !h.rateLimiter.Allow(clientIP) {
		h.writeError(w, "rate limit exceeded", http.StatusTooManyRequests)
		return
	}

	ipStr := r.URL.Query().Get("ip")
	if ipStr == "" {
		h.writeError(w, "missing 'ip' query parameter", http.StatusBadRequest)
		return
	}

	ip := net.ParseIP(ipStr)
	if ip == nil {
		h.writeError(w, "invalid IP address format", http.StatusBadRequest)
		return
	}

	location, err := h.db.FindLocation(ip)
	if err != nil {
		if _, ok := err.(*database.NotFoundError); ok {
			h.writeError(w, "IP address not found", http.StatusNotFound)
			return
		}
		log.Printf("Database error: %v", err)
		h.writeError(w, "internal server error", http.StatusInternalServerError)
		return
	}

	h.writeJSON(w, http.StatusOK, map[string]string{
		"country": location.Country,
		"city":    location.City,
	})
}

func (h *Handler) writeJSON(w http.ResponseWriter, statusCode int, data interface{}) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(statusCode)
	if err := json.NewEncoder(w).Encode(data); err != nil {
		log.Printf("Failed to encode JSON response: %v", err)
	}
}

func (h *Handler) writeError(w http.ResponseWriter, message string, statusCode int) {
	h.writeJSON(w, statusCode, map[string]string{
		"error": message,
	})
}

func getClientIP(r *http.Request) string {
	// check proxy headers first
	if xff := r.Header.Get("X-Forwarded-For"); xff != "" {
		return xff
	}
	if xri := r.Header.Get("X-Real-IP"); xri != "" {
		return xri
	}
	// fallback to RemoteAddr
	ip, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return r.RemoteAddr
	}
	return ip
}

