# nim-libp2p Relay Server <!-- omit in toc -->

[![libp2p.io](https://img.shields.io/badge/project-libp2p-yellow.svg?style=flat-square)](http://libp2p.io/)
[![Discuss](https://img.shields.io/discourse/https/discuss.libp2p.io/posts.svg?style=flat-square)](https://discuss.libp2p.io)

> A circuit relay server implementation in Nim using nim-libp2p, enabling NAT traversal and peer-to-peer connections for browser-based libp2p applications

## Table of Contents <!-- omit in toc -->

- [Overview](#overview)
- [Architecture](#architecture)
- [Prerequisites](#prerequisites)
- [Setup](#setup)
- [Usage](#usage)
  - [Running the Relay](#running-the-relay)
  - [Configuration](#configuration)
- [How It Works](#how-it-works)
  - [Circuit Relay](#circuit-relay)
  - [GossipSub Auto-Discovery](#gossipsub-auto-discovery)
  - [Protocols Enabled](#protocols-enabled)
  - [HTTP API](#http-api)
- [Key Features](#key-features)
- [Project Structure](#project-structure)
- [Need help?](#need-help)
- [License](#license)
- [Contribution](#contribution)

## Overview

This is a production-ready circuit relay server implementation written in Nim using [nim-libp2p](https://github.com/vacp2p/nim-libp2p). It enables browser-based libp2p applications to establish peer-to-peer connections through NAT and firewall restrictions.

Key features:

- **Custom Yjs Provider**: A libp2p-based connection provider for Yjs (`yjs-libp2p-provider.js`)
- **WebRTC Support**: Direct peer-to-peer connections using WebRTC
- **Circuit Relay**: NAT traversal via relay servers
- **AutoNAT**: Automatic NAT detection
- **PubSub**: GossipSub for document synchronization
- **Peer Discovery**: Automatic connection to discovered peers via pubsub peer discovery

## Architecture

```
┌─────────────┐         ┌─────────────┐
│  Browser 1  │         │  Browser 2  │
│             │         │             │
│  Yjs Doc ←──┼─────────┼──→ Yjs Doc  │
│     ↕       │  WebRTC │      ↕      │
│  libp2p     │    or   │   libp2p    │
│  (pubsub)   │  Relay  │  (pubsub)   │
└──────┬──────┘         └──────┬──────┘
       │                       │
       │    ┌─────────────┐    │
       └────┤   Nim Relay │────┘
            │   Server    │
            │             │
            │ - Circuit   │
            │   Relay v2  │
            │ - GossipSub │
            │ - AutoNAT   │
            │ - HTTP API  │
            └─────────────┘
         Port 9091 (TCP)
         Port 9092 (WebSocket)
         Port 9094 (HTTP API)
```

## Prerequisites

- [Nim](https://nim-lang.org/) >= 2.0.16
- [Nimble](https://github.com/nim-lang/nimble) (Nim's package manager)

## Setup

1. Clone the repository:
```bash
git clone <repository-url>
cd nim-libp2p-example
```

2. Install dependencies:
```bash
nimble install -d
```

3. Build the relay:
```bash
nimble build
```

## Usage

### Running the Relay

**Standard mode:**
```bash
nimble run
```

**Or using the binary directly:**
```bash
./main
```

The relay will:
1. Load or generate a persistent peer ID (stored in `relay-peer-id.json`)
2. Start listening on:
   - TCP: `0.0.0.0:9091`
   - WebSocket: `0.0.0.0:9092`
   - HTTP API: `0.0.0.0:9094`
3. Print its multiaddrs (e.g., `/ip4/127.0.0.1/tcp/9092/ws/p2p/12D3Koo...`)
4. Begin accepting circuit relay connections

### Configuration

Edit `relay_constants.nim` to customize:

```nim
const
  DISCOVERY_TOPIC* = "discovery"    # Peer discovery topic
  DebugEnabled* = true              # Enable debug logging
```

The relay automatically:
- Discovers and subscribes to topics that connected peers use
- Forwards GossipSub messages between peers
- Enables circuit relay for NAT traversal

## HTTP API

The relay exposes a REST API on port 9094:

### GET /api/addresses

Returns the relay's multiaddresses in JSON format:

```bash
curl http://localhost:9094/api/addresses
```

Response:
```json
{
  "websocket": [
    "/ip4/127.0.0.1/tcp/9092/ws/p2p/12D3Koo...",
    "/ip4/10.0.0.1/tcp/9092/ws/p2p/12D3Koo..."
  ],
  "tcp": [
    "/ip4/127.0.0.1/tcp/9091/p2p/12D3Koo...",
    "/ip4/10.0.0.1/tcp/9091/p2p/12D3Koo..."
  ],
  "all": [...]
}
```

Browser clients can fetch this endpoint to discover available relay addresses.

## How It Works

### Circuit Relay

The relay implements [Circuit Relay v2](https://github.com/libp2p/specs/blob/master/relay/circuit-v2.md), allowing browser peers to:

1. **Connect to the relay** via WebSocket
2. **Reserve relay slots** for incoming connections
3. **Establish connections** to other peers through the relay
4. **Upgrade to direct connections** when possible (using DCUtR)

### GossipSub Auto-Discovery

The relay automatically discovers and subscribes to topics:

```nim
# Every 2 seconds, check GossipSub mesh for new topics
for topic, peers in gossip.mesh.pairs:
  if peers.len > 0 and topic not in subscribedTopics:
    gossip.subscribe(topic, nil)
    info "🎯 Auto-subscribed to peer topic", topic = topicName
```

This ensures the relay can forward messages for any topic that connected peers use.

### Protocols Enabled

- **Circuit Relay v2**: NAT traversal and connection establishment
- **GossipSub**: Topic-based pub/sub messaging with flood publishing
- **Identify**: Peer identification and address advertisement
- **AutoNAT**: Automatic NAT detection for peers
- **Ping**: Connectivity testing
- **Noise**: Secure channel encryption
- **Yamux**: Stream multiplexing

### HTTP API

A lightweight HTTP server (using chronos) provides:
- Address discovery endpoint
- CORS support for browser clients
- JSON responses for easy integration

## Key Features

- **Circuit Relay v2**: Full relay server functionality
- **Auto-Discovery**: Automatically subscribes to peer topics
- **Persistent Identity**: Peer ID maintained across restarts
- **Multiple Transports**: TCP and WebSocket support
- **HTTP API**: Easy address discovery for clients
- **Cross-Platform**: Works on Linux, macOS, Windows

## Project Structure

```
nim-libp2p-example/
├── main.nim              # Main relay server implementation
├── peerid.nim           # Persistent peer ID management
├── relay_constants.nim  # Configuration constants
├── relay-peer-id.json  #  peer identity 
├── nim_libp2p_example.nimble  # Nim package config
└── README.md           # This file
```

### Key Components

**`main.nim`**:
- Creates libp2p host with SwitchBuilder
- Configures transports (TCP, WebSocket)
- Enables protocols (Relay, GossipSub, AutoNAT, etc.)
- Implements HTTP API server
- Auto-subscribes to discovered topics

**`peerid.nim`**:
- Loads existing Ed25519 keypair from JSON
- Generates new keypair if none exists
- Saves in libp2p-compatible format

## Need help?

- Read the [nim-libp2p documentation](https://github.com/vacp2p/nim-libp2p)
- Check out the [nim-libp2p API docs](https://vacp2p.github.io/nim-libp2p/docs/)
- Check out the [general libp2p documentation](https://docs.libp2p.io) for tips, how-tos and more
- Read the [libp2p specs](https://github.com/libp2p/specs)
- Read the [Circuit Relay v2 spec](https://github.com/libp2p/specs/blob/master/relay/circuit-v2.md)
- Ask questions on the [libp2p discussion board](https://discuss.libp2p.io)
- Check the [Nim community](https://nim-lang.org/community.html)

## License

Licensed under either of

- Apache 2.0, ([LICENSE-APACHE](LICENSE-APACHE) / <http://www.apache.org/licenses/LICENSE-2.0>)
- MIT ([LICENSE-MIT](LICENSE-MIT) / <http://opensource.org/licenses/MIT>)

## Contribution

Unless you explicitly state otherwise, any contribution intentionally submitted
for inclusion in the work by you, as defined in the Apache-2.0 license, shall be
dual licensed as above, without any additional terms or conditions.
