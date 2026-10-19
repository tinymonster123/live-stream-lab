package main

import (
	"encoding/json"
	"log"
	"net/http"
	"os"
)

type healthResponse struct {
	Status  string `json:"status"`
	Service string `json:"service"`
}

func newMux() *http.ServeMux {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", handleHealth)
	return mux
}

func handleHealth(writer http.ResponseWriter, _ *http.Request) {
	writer.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(writer).Encode(healthResponse{Status: "ok", Service: "control-plane"}); err != nil {
		log.Printf("encode health response: %v", err)
	}
}

func main() {
	address := os.Getenv("CONTROL_PLANE_ADDR")
	if address == "" {
		address = ":8081"
	}

	server := &http.Server{Addr: address, Handler: newMux()}

	log.Printf("control-plane listening on %s", address)
	if err := server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		log.Fatal(err)
	}
}
