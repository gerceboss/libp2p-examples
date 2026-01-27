package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"

	"github.com/libp2p/go-libp2p/core/host"
)

const (
	HTTP_PORT = 9094
	DEBUG     = false // Set via environment variable
)

func main() {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	privKey, peerID, err := loadOrGeneratePeerID()
	if err != nil {
		log.Fatalf("Failed to load/generate peer ID: %v", err)
	}

	h, err := createLibp2pHost(privKey)
	if err != nil {
		log.Fatalf("Failed to create libp2p host: %v", err)
	}
	defer h.Close()

	if h.ID() != peerID {
		log.Printf("Warning: Host peer ID (%s) doesn't match loaded peer ID (%s)", h.ID(), peerID)
	}

	if err := setupRelay(ctx, h); err != nil {
		log.Fatalf("Failed to set up relay services: %v", err)
	}

	// Print listening addresses
	fmt.Println("\nRelay listening on:")
	for _, addr := range h.Addrs() {
		fmt.Printf("  %s/p2p/%s\n", addr, h.ID())
	}

	// Start HTTP server for API
	go startHTTPServer(h)

	// Wait for interrupt signal
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, os.Interrupt, syscall.SIGTERM)
	<-sigChan

	fmt.Println("\nShutting down...")
}

type addressesResponse struct {
	Websocket    []string `json:"websocket"`
	WebRTCDirect []string `json:"webrtcDirect"`
	TCP          []string `json:"tcp"`
	All          []string `json:"all"`
}

// HTTP server to serve multiaddrs
func startHTTPServer(h host.Host) {
	http.HandleFunc("/api/addresses", func(w http.ResponseWriter, r *http.Request) {
		// Enable CORS
		w.Header().Set("Access-Control-Allow-Origin", "*")
		w.Header().Set("Access-Control-Allow-Methods", "GET, OPTIONS")
		w.Header().Set("Access-Control-Allow-Headers", "Content-Type")

		if r.Method == "OPTIONS" {
			w.WriteHeader(http.StatusNoContent)
			return
		}

		if r.Method != "GET" {
			http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
			return
		}

		// Get all multiaddrs
		multiaddrs := make([]string, 0)
		for _, addr := range h.Addrs() {
			multiaddrs = append(multiaddrs, fmt.Sprintf("%s/p2p/%s", addr, h.ID()))
		}

		addresses := addressesResponse{
			Websocket:    make([]string, 0),
			WebRTCDirect: make([]string, 0),
			TCP:          make([]string, 0),
			All:          multiaddrs,
		}

		for _, ma := range multiaddrs {
			if strings.Contains(ma, "/ws") || strings.Contains(ma, "/wss") {
				addresses.Websocket = append(addresses.Websocket, ma)
			} else if strings.Contains(ma, "/webrtc-direct") {
				addresses.WebRTCDirect = append(addresses.WebRTCDirect, ma)
			} else if strings.Contains(ma, "/tcp/") && !strings.Contains(ma, "/ws") {
				addresses.TCP = append(addresses.TCP, ma)
			}
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(addresses)
	})

	http.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/" {
			http.NotFound(w, r)
			return
		}
		w.WriteHeader(http.StatusNotFound)
		w.Write([]byte("Not found. Try /api/addresses"))
	})

	fmt.Printf("\nHTTP API listening on: http://0.0.0.0:%d\n", HTTP_PORT)
	fmt.Printf("Get addresses: http://localhost:%d/api/addresses\n\n", HTTP_PORT)

	if err := http.ListenAndServe(fmt.Sprintf(":%d", HTTP_PORT), nil); err != nil {
		log.Fatalf("Failed to start HTTP server: %v", err)
	}
}
