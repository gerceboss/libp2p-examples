package main

import "time"

// Relay configuration constants matching JS version

// Relay server timeouts
var RELAY_TIMEOUTS = struct {
	HOP_TIMEOUT                   time.Duration
	PROTOCOL_NEGOTIATION_INBOUND  time.Duration
	PROTOCOL_NEGOTIATION_OUTBOUND time.Duration
	UPGRADE_INBOUND               time.Duration
	UPGRADE_OUTBOUND              time.Duration
	DIAL_TIMEOUT                  time.Duration
}{
	HOP_TIMEOUT:                   30 * time.Second,
	PROTOCOL_NEGOTIATION_INBOUND:  30 * time.Second,
	PROTOCOL_NEGOTIATION_OUTBOUND: 30 * time.Second,
	UPGRADE_INBOUND:               30 * time.Second,
	UPGRADE_OUTBOUND:              30 * time.Second,
	DIAL_TIMEOUT:                  30 * time.Second,
}

// Relay server reservation configuration
var RELAY_RESERVATIONS = struct {
	MAX_RESERVATIONS       int
	RESERVATION_TTL        time.Duration
	DEFAULT_DATA_LIMIT     uint64
	DEFAULT_DURATION_LIMIT time.Duration
}{
	MAX_RESERVATIONS:       1000,
	RESERVATION_TTL:        2 * time.Hour,
	DEFAULT_DATA_LIMIT:     1024 * 1024 * 1024, // 1 GB
	DEFAULT_DURATION_LIMIT: 2 * time.Minute,
}

// Connection manager configuration
var CONNECTION_CONFIG = struct {
	MAX_CONNECTIONS        int
	MAX_INCOMING_PENDING   int
	MAX_PEER_ADDRS_TO_DIAL int
}{
	MAX_CONNECTIONS:        1000,
	MAX_INCOMING_PENDING:   100,
	MAX_PEER_ADDRS_TO_DIAL: 100,
}

// Peer discovery configuration
var DISCOVERY_CONFIG = struct {
	INTERVAL time.Duration
	TOPICS   []string
}{
	INTERVAL: 10 * time.Second,
	TOPICS:   []string{"_peer-discovery._p2p._pubsub"},
}

// Monitoring intervals
var MONITORING = struct {
	TOPIC_STATUS_INTERVAL time.Duration
}{
	TOPIC_STATUS_INTERVAL: 10 * time.Second,
}

// Default Yjs topic to subscribe
const DEFAULT_TOPIC = "yjs-doc-1"

// Discovery topic constant
const DISCOVERY_TOPIC = "_peer-discovery._p2p._pubsub"
