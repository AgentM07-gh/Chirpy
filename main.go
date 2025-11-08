package main

import (
	"fmt"
	"net/http"
	"sync/atomic"
)

// This struct holds data the server needs to remember
type apiConfig struct {
	fileserverHits atomic.Int32 // Thread-safe counter
}

// Middleware that increments the counter
func (cfg *apiConfig) middlewareMetricsInc(next http.Handler) http.Handler {
	// Return a new handler that wraps the original
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Before calling the original handler: increment counter
		cfg.fileserverHits.Add(1)

		// Call the original handler
		next.ServeHTTP(w, r)

		// After would go here (but we don't need it)
	})
}

// Handler that shows the metrics
func (cfg *apiConfig) metricsHandler(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	w.WriteHeader(200)

	// Load the current count (thread-safe read)
	hits := cfg.fileserverHits.Load()

	// Format and write the response
	response := fmt.Sprintf("Hits: %d", hits)
	w.Write([]byte(response))
}

// Handler that resets the counter
func (cfg *apiConfig) resetHandler(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	w.WriteHeader(200)

	// Reset to zero (thread-safe write)
	cfg.fileserverHits.Store(0)

	w.Write([]byte("Counter reset"))
}

func main() {
	// Create the config struct (our memory box)
	apiCfg := &apiConfig{}

	mux := http.NewServeMux()

	// Health check (same as Lab 4)
	mux.HandleFunc("/healthz", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		w.WriteHeader(200)
		w.Write([]byte("OK"))
	})

	// Metrics endpoint - shows the count
	mux.HandleFunc("/metrics", apiCfg.metricsHandler)

	// Reset endpoint - resets the count
	mux.HandleFunc("/reset", apiCfg.resetHandler)

	// File server wrapped with middleware
	fileServer := http.FileServer(http.Dir("."))
	strippedHandler := http.StripPrefix("/app", fileServer)

	// Wrap with middleware - now every request increments counter
	wrappedHandler := apiCfg.middlewareMetricsInc(strippedHandler)

	mux.Handle("/app/", wrappedHandler)

	server := &http.Server{
		Addr:    ":8080",
		Handler: mux,
	}

	server.ListenAndServe()
}
