package main

import (
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"sync/atomic"
)

type apiConfig struct {
	fileserverHits atomic.Int32
}

func main() {
	const filepathRoot = "."
	const port = "8080"

	apiCfg := apiConfig{
		fileserverHits: atomic.Int32{},
	}

	mux := http.NewServeMux()

	fileServer := http.FileServer(http.Dir(filepathRoot))
	strippedHandler := http.StripPrefix("/app", fileServer)
	mux.Handle("/app/", apiCfg.middlewareMetricsInc(strippedHandler))

	mux.HandleFunc("GET /api/healthz", healthzHandler)
	mux.HandleFunc("GET /admin/metrics", apiCfg.metricsHandler)
	mux.HandleFunc("POST /admin/reset", apiCfg.resetHandler)
	mux.HandleFunc("POST /api/validate_chirp", validateHandler)

	server := &http.Server{
		Addr:    ":" + port,
		Handler: mux,
	}
	log.Fatal(server.ListenAndServe())
}

func validateHandler(w http.ResponseWriter, r *http.Request) {
	type Chirp struct {
		Body string `json:"body"`
	}
	type ValidResponse struct {
		Valid bool `json:"valid"`
	}
	type ErrorResponse struct {
		Error string `json:"error"`
	}

	decoder := json.NewDecoder(r.Body)
	chirp := Chirp{}
	format_err := ErrorResponse{Error: "Something went wrong"}
	len_err := ErrorResponse{Error: "Chirp is too long"}
	valid := ValidResponse{Valid: true}
	err := decoder.Decode(&chirp)
	if err != nil {
		dat, err := json.Marshal(format_err)
		if err != nil {
			log.Printf("Error marshalling JSON: %s", err)
			w.WriteHeader(500)
			return
		} else {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(400)
			w.Write(dat)
			return
		}
	} else if len(chirp.Body) > 140 {
		dat, err := json.Marshal(len_err)
		if err != nil {
			log.Printf("Error marshalling JSON: %s", err)
			w.WriteHeader(500)
			return
		} else {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(400)
			w.Write(dat)
			return
		}
	}

	dat, err := json.Marshal(valid)
	if err != nil {
		log.Printf("Error marshalling JSON: %s", err)
		w.WriteHeader(500)
		return
	} else {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(200)
		w.Write(dat)
		return
	}
}

func healthzHandler(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	w.WriteHeader(http.StatusOK)
	w.Write([]byte("OK"))
}

func (cfg *apiConfig) metricsHandler(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(http.StatusOK)
	hits := cfg.fileserverHits.Load()
	message := fmt.Sprintf(`
	<html>
  		<body>
    		<h1>Welcome, Chirpy Admin</h1>
    		<p>Chirpy has been visited %d times!</p>
  		</body>
	</html>`, hits)
	w.Write([]byte(message))
}

func (cfg *apiConfig) resetHandler(w http.ResponseWriter, r *http.Request) {
	cfg.fileserverHits.Store(0)
	w.WriteHeader(http.StatusOK)
}

func (cfg *apiConfig) middlewareMetricsInc(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		cfg.fileserverHits.Add(1)
		next.ServeHTTP(w, r)
	})
}
