package main

import (
	"log"
	"net/http"
)

func main() {

	ServeMux := http.NewServeMux()

	ServeMux.Handle("/app/", http.StripPrefix("/app", http.FileServer(http.Dir("."))))
	ServeMux.HandleFunc("/healthz", handlerReadiness)

	server := &http.Server{
		Handler: ServeMux,
		Addr:    ":8080",
	}

	log.Println("Server is listening on port 8080...")
	// Start the server.
	if err := server.ListenAndServe(); err != nil {
		log.Fatalf("Server failed: %v", err)
	}
}

func handlerReadiness(w http.ResponseWriter, r *http.Request) {
	w.Header().Add("Content-Type", "text/plain; charset=utf-8")
	w.WriteHeader(http.StatusOK)
	w.Write([]byte(http.StatusText(http.StatusOK)))
}
