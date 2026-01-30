## Relay configuration constants

import std/[os, strutils]
import chronos

let DebugEnabled* = getEnv("DEBUG", "").toLowerAscii() in ["1", "true", "yes"]

# Topics
const
  DEFAULT_TOPIC* = "yjs-doc-1"
  DISCOVERY_TOPIC* = "_peer-discovery._p2p._pubsub"

# Relay server timeouts
const
  HOP_TIMEOUT* = 30.seconds
  PROTOCOL_NEGOTIATION_INBOUND* = 30.seconds
  PROTOCOL_NEGOTIATION_OUTBOUND* = 30.seconds
  UPGRADE_INBOUND* = 30.seconds
  UPGRADE_OUTBOUND* = 30.seconds
  DIAL_TIMEOUT* = 30.seconds

# Relay server reservation configuration
const
  MAX_RESERVATIONS* = 1000
  RESERVATION_TTL* = 2.hours
  DEFAULT_DATA_LIMIT* = 1024'u64 * 1024'u64 * 1024'u64 # 1 GiB
  DEFAULT_DURATION_LIMIT* = 2.minutes

# Connection manager configuration
const
  MAX_CONNECTIONS* = 1000
  MAX_INCOMING_PENDING* = 100
  MAX_PEER_ADDRS_TO_DIAL* = 100

# Peer discovery configuration
const
  DISCOVERY_INTERVAL* = 10.seconds
  DISCOVERY_TOPICS* = [DISCOVERY_TOPIC]

# Monitoring intervals
const
  TOPIC_STATUS_INTERVAL* = 10.seconds