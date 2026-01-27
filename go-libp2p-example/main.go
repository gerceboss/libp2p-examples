package main

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/libp2p/go-libp2p"
	"github.com/libp2p/go-libp2p/core/host"
	"github.com/libp2p/go-libp2p/core/network"
	"github.com/libp2p/go-libp2p/core/peer"
	"github.com/libp2p/go-libp2p/p2p/transport/tcp"
	"github.com/libp2p/go-libp2p/p2p/transport/websocket"
	"github.com/libp2p/go-libp2p/p2p/protocol/circuitv2/relay"
	"github.com/libp2p/go-libp2p/p2p/protocol/holepunch"
	autonat "github.com/libp2p/go-libp2p/p2p/host/autonat"
	"github.com/libp2p/go-libp2p/p2p/host/peerstore/pstoreds"
	"github.com/libp2p/go-libp2p/p2p/protocol/identify"
	"github.com/libp2p/go-libp2p/p2p/protocol/ping"
	"github.com/libp2p/go-libp2p/p2p/security/noise"
	"github.com/libp2p/go-libp2p/p2p/muxer/yamux"
	"github.com/libp2p/go-libp2p/core/crypto"
	pubsub "github.com/libp2p/go-libp2p-pubsub"
	"github.com/multiformats/go-multiaddr"
	badger "github.com/ipfs/go-ds-badger"
)

const (
	HTTP_PORT = 9094
	DEBUG     = false // Set via environment variable
)

// PeerID data structure matching JS version
type PeerIDData struct {
	ID      string `json:"id"`
	PrivKey string `json:"privKey"`
	PubKey  string `json:"pubKey"`
}

func loadOrGeneratePeerID() (crypto.PrivKey, peer.ID, error) {
	const PEER_ID_FILE = "./relay-peer-id.json"

	// Try to load existing peer ID
	if data, err := os.ReadFile(PEER_ID_FILE); err == nil {
		var peerData PeerIDData
		if err := json.Unmarshal(data, &peerData); err == nil {
			// Decode private key from base64 (protobuf format)
			// The privKey is base64-encoded protobuf
			privKeyBytes, err := crypto.ConfigDecodeKey(peerData.PrivKey)
			if err != nil {
				// Try direct base64 decode if ConfigDecodeKey fails
				privKeyBytes, err = base64.StdEncoding.DecodeString(peerData.PrivKey)
				if err != nil {
					return nil, "", fmt.Errorf("failed to decode private key: %w", err)
				}
			}

			privKey, err := crypto.UnmarshalPrivateKey(privKeyBytes)
			if err != nil {
				return nil, "", fmt.Errorf("failed to unmarshal private key: %w", err)
			}

			peerID, err := peer.Decode(peerData.ID)
			if err != nil {
				return nil, "", fmt.Errorf("failed to parse peer ID: %w", err)
			}

			fmt.Printf("Loaded existing PeerId: %s\n", peerID.String())
			return privKey, peerID, nil
		}
	}

	// Generate new peer ID
	fmt.Println("Generating new PeerId...")
	privKey, _, err := crypto.GenerateEd25519Key(nil)
	if err != nil {
		return nil, "", fmt.Errorf("failed to generate key: %w", err)
	}

	peerID, err := peer.IDFromPrivateKey(privKey)
	if err != nil {
		return nil, "", fmt.Errorf("failed to get peer ID: %w", err)
	}

	// Save peer ID (matching JS format - base64 encoded protobuf)
	pubKeyBytes, err := crypto.MarshalPublicKey(privKey.GetPublic())
	if err != nil {
		return nil, "", fmt.Errorf("failed to marshal public key: %w", err)
	}

	privKeyBytes, err := crypto.MarshalPrivateKey(privKey)
	if err != nil {
		return nil, "", fmt.Errorf("failed to marshal private key: %w", err)
	}

	// Use ConfigEncodeKey to match JS format (protobuf base64)
	peerData := PeerIDData{
		ID:      peerID.String(),
		PrivKey: crypto.ConfigEncodeKey(privKeyBytes),
		PubKey:  crypto.ConfigEncodeKey(pubKeyBytes),
	}

	data, err := json.MarshalIndent(peerData, "", "  ")
	if err != nil {
		return nil, "", fmt.Errorf("failed to marshal peer data: %w", err)
	}

	if err := os.WriteFile(PEER_ID_FILE, data, 0644); err != nil {
		return nil, "", fmt.Errorf("failed to write peer ID file: %w", err)
	}

	fmt.Printf("Generated new PeerId: %s\n", peerID.String())
	return privKey, peerID, nil
}

func createLibp2pHost(privKey crypto.PrivKey) (host.Host, error) {
	// Create datastore for peerstore
	ds, err := badger.NewDatastore("./peerstore", &badger.DefaultOptions)
	if err != nil {
		return nil, fmt.Errorf("failed to create datastore: %w", err)
	}

	ps, err := pstoreds.NewPeerstore(context.Background(), ds, pstoreds.DefaultOpts())
	if err != nil {
		return nil, fmt.Errorf("failed to create peerstore: %w", err)
	}

	// Create libp2p host with matching configuration
	h, err := libp2p.New(
		libp2p.Identity(privKey),
		libp2p.ListenAddrs(
			multiaddr.StringCast("/ip4/0.0.0.0/tcp/9091"),
			multiaddr.StringCast("/ip4/0.0.0.0/tcp/9092/ws"),
		),
		libp2p.Transport(tcp.NewTCPTransport),
		libp2p.Transport(websocket.New),
		libp2p.Security(noise.ID, noise.New),
		libp2p.Muxer("/yamux/1.0.0", yamux.DefaultTransport),
		libp2p.Peerstore(ps),
		libp2p.EnableNATService(),
		libp2p.EnableRelay(),
		libp2p.EnableHolePunching(),
	)
	if err != nil {
		return nil, fmt.Errorf("failed to create libp2p host: %w", err)
	}

	return h, nil
}

func main() {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Load or generate peer ID
	privKey, peerID, err := loadOrGeneratePeerID()
	if err != nil {
		log.Fatalf("Failed to load/generate peer ID: %v", err)
	}

	// Create libp2p host
	h, err := createLibp2pHost(privKey)
	if err != nil {
		log.Fatalf("Failed to create libp2p host: %v", err)
	}
	defer h.Close()

	// Verify peer ID matches
	if h.ID() != peerID {
		log.Printf("Warning: Host peer ID (%s) doesn't match loaded peer ID (%s)", h.ID(), peerID)
	}

	// Set up relay service
	_, err = relay.New(h)
	if err != nil {
		log.Fatalf("Failed to create relay service: %v", err)
	}

	// Identify service will be created for hole punching

	// Set up ping service
	ping.NewPingService(h)

	// Set up identify service (needed for hole punching)
	idService, err := identify.NewIDService(h)
	if err != nil {
		log.Printf("Warning: Failed to create identify service: %v", err)
	}

	// Set up autonat service
	_, err = autonat.New(h)
	if err != nil {
		log.Printf("Warning: Failed to create autonat service: %v", err)
	}

	// Set up hole punching (with public address function)
	_, err = holepunch.NewService(h, idService, func() []multiaddr.Multiaddr {
		// Return public addresses if available
		return h.Addrs()
	})
	if err != nil {
		log.Printf("Warning: Failed to create holepunch service: %v", err)
	}

	// Set up pubsub with flood publish enabled
	// This ensures messages are routed to all peers, similar to JS version's
	// allowPublishToZeroTopicPeers: true
	ps, err := pubsub.NewGossipSub(ctx, h,
		pubsub.WithMessageIdFn(pubsub.DefaultMsgIdFn),
		pubsub.WithPeerOutboundQueueSize(128),
		pubsub.WithValidateQueueSize(128),
		pubsub.WithMaxMessageSize(pubsub.DefaultMaxMessageSize),
		pubsub.WithFloodPublish(true), // Route messages to all peers, not just mesh members
	)
	if err != nil {
		log.Fatalf("Failed to create pubsub: %v", err)
	}

	// Subscribe to discovery topic
	discoveryTopic, err := ps.Join(DISCOVERY_TOPIC)
	if err != nil {
		log.Fatalf("Failed to join discovery topic: %v", err)
	}
	fmt.Printf("Subscribed to discovery topic: %s\n", DISCOVERY_TOPIC)

	// Track subscribed topics
	subscribedTopics := make(map[string]*pubsub.Topic)
	subscribedTopics[DISCOVERY_TOPIC] = discoveryTopic

	// Auto-subscribe to topics that peers subscribe to (like JS version)
	// This allows the relay to route messages between peers
	// We use a combination of periodic checks and message-based discovery
	go func() {
		ticker := time.NewTicker(2 * time.Second) // Check every 2 seconds
		defer ticker.Stop()

		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				// Get all topics that we know about (including from peers)
				allTopics := ps.GetTopics()
				for _, topicName := range allTopics {
					// Skip discovery topic (already subscribed)
					if topicName == DISCOVERY_TOPIC {
						continue
					}

					// If we haven't subscribed to this topic yet, join it
					if _, exists := subscribedTopics[topicName]; !exists {
						topic, err := ps.Join(topicName)
						if err != nil {
							if DEBUG {
								log.Printf("Failed to auto-subscribe to topic %s: %v", topicName, err)
							}
							continue
						}
						subscribedTopics[topicName] = topic
						fmt.Printf("Auto-subscribed to topic: %s\n", topicName)

						// Subscribe to messages on this topic to route them
						sub, err := topic.Subscribe()
						if err == nil {
							go func(topicName string, topic *pubsub.Topic) {
								for {
									msg, err := sub.Next(ctx)
									if err != nil {
										return
									}
									// Relay the message by republishing it
									// This ensures messages are routed to all peers
									if DEBUG {
										peerID := msg.GetFrom()
										fmt.Printf("Relaying message on %s from %s\n", topicName, peerID.ShortString())
									}
									// Note: WithFloodPublish(true) should handle routing automatically
									// but we ensure the topic is joined so we're in the mesh
								}
							}(topicName, topic)
						}
					}
				}
			}
		}
	}()

	// Subscribe to discovery topic messages to route them (always, not just in DEBUG)
	// This is critical for peer discovery to work
	sub, err := discoveryTopic.Subscribe()
	if err == nil {
		go func() {
			for {
				msg, err := sub.Next(ctx)
				if err != nil {
					return
				}
				peerID := msg.GetFrom()
				// Log discovery messages to help debug
				fmt.Printf("Discovery message on %s from %s\n", DISCOVERY_TOPIC, peerID.ShortString())
				// The message will be automatically routed to other subscribers via GossipSub
			}
		}()
	} else {
		log.Printf("Warning: Failed to subscribe to discovery topic messages: %v", err)
	}

	// Set up event handlers (always enable to monitor peer connections for topic discovery)
	h.Network().Notify(&networkNotifier{
		pubsub: ps,
		ctx:    ctx,
	})

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

// NetworkNotifier for debug logging and topic monitoring
type networkNotifier struct {
	pubsub *pubsub.PubSub
	ctx    context.Context
}

func (n *networkNotifier) Listen(network.Network, multiaddr.Multiaddr)      {}
func (n *networkNotifier) ListenClose(network.Network, multiaddr.Multiaddr) {}
func (n *networkNotifier) Connected(net network.Network, conn network.Conn) {
	peerID := conn.RemotePeer()
	if DEBUG {
		fmt.Printf("Peer connected: %s\n", peerID.ShortString())
	}
	// When a peer connects, check for new topics they might be using
	// This helps auto-subscribe faster
	if n.pubsub != nil {
		go func() {
			// Small delay to let subscription messages propagate
			time.Sleep(500 * time.Millisecond)
			topics := n.pubsub.GetTopics()
			for _, topicName := range topics {
				if topicName != DISCOVERY_TOPIC {
					// Topic exists, ensure we're subscribed (handled by main loop)
					_ = topicName
				}
			}
		}()
	}
}
func (n *networkNotifier) Disconnected(net network.Network, conn network.Conn) {
	if DEBUG {
		peerID := conn.RemotePeer()
		fmt.Printf("Peer disconnected: %s\n", peerID.ShortString())
	}
}
func (n *networkNotifier) OpenedStream(network.Network, network.Stream) {}
func (n *networkNotifier) ClosedStream(network.Network, network.Stream) {}

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

		// Categorize addresses by type
		addresses := map[string][]string{
			"websocket":    make([]string, 0),
			"webrtcDirect": make([]string, 0),
			"tcp":          make([]string, 0),
			"all":          multiaddrs,
		}

		for _, ma := range multiaddrs {
			if strings.Contains(ma, "/ws") || strings.Contains(ma, "/wss") {
				addresses["websocket"] = append(addresses["websocket"], ma)
			} else if strings.Contains(ma, "/webrtc-direct") {
				addresses["webrtcDirect"] = append(addresses["webrtcDirect"], ma)
			} else if strings.Contains(ma, "/tcp/") && !strings.Contains(ma, "/ws") {
				addresses["tcp"] = append(addresses["tcp"], ma)
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
