package main

import (
	"log"
	"net/http"
)

func main() {

	ServeMux := http.NewServeMux()

	ServeMux.Handle("/", http.FileServer(http.Dir(".")))

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
