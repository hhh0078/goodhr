package main

import (
	"log"
	"net/http"
	"os"

	"goodhr5/cloud/backend/internal/httpapi"
)

func main() {
	addr := envOrDefault("GOODHR_CLOUD_ADDR", ":8080")
	server := httpapi.NewServer()

	log.Printf("GoodHR 5 cloud backend listening on %s", addr)
	if err := http.ListenAndServe(addr, server.Routes()); err != nil {
		log.Fatal(err)
	}
}

func envOrDefault(key string, fallback string) string {
	value := os.Getenv(key)
	if value == "" {
		return fallback
	}
	return value
}
