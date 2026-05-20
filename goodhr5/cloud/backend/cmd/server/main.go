// 本文件负责启动 GoodHR 5 云端 HTTP 服务。
package main

import (
	"log"
	"net/http"
	"os"

	"goodhr5/cloud/backend/internal/httpapi"
)

func main() {
	addr := envOrDefault("GOODHR_CLOUD_ADDR", ":8084")
	server, err := httpapi.NewServer()
	if err != nil {
		log.Fatal(err)
	}

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
