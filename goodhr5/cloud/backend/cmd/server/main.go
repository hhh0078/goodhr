package main

import (
	"encoding/json"
	"log"
	"net/http"
	"os"
)

type healthResponse struct {
	OK      bool   `json:"ok"`
	Name    string `json:"name"`
	Version string `json:"version"`
}

func main() {
	mux := http.NewServeMux()
	mux.HandleFunc("/health", healthHandler)

	addr := envOrDefault("GOODHR_CLOUD_ADDR", ":8080")
	log.Printf("GoodHR 5 cloud backend listening on %s", addr)
	if err := http.ListenAndServe(addr, mux); err != nil {
		log.Fatal(err)
	}
}

func healthHandler(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	_ = json.NewEncoder(w).Encode(healthResponse{
		OK:      true,
		Name:    "GoodHR 5 Cloud API",
		Version: "0.1.0",
	})
}

func envOrDefault(key string, fallback string) string {
	value := os.Getenv(key)
	if value == "" {
		return fallback
	}
	return value
}
