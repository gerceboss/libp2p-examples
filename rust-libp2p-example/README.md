# Yjs + libp2p Collaborative Spreadsheet (Rust relay + browser clients)

> A collaborative spreadsheet built with Yjs and libp2p, demonstrating real-time peer-to-peer document synchronization.

This repo uses:

- **Browser client**: the same Yjs + js-libp2p frontend as [js-libp2p-example-yjs-libp2p](https://github.com/NiKrause/js-libp2p-examples/tree/main/examples/js-libp2p-example-yjs-libp2p)
- **Relay server**: a **Rust** libp2p relay (`src/main.rs`) that replaces the JavaScript `relay.js`

The Rust relay mirrors `relay.js`: same listen addresses (TCP 9091, WebSocket 9092), HTTP API on 9094 (`/api/addresses`), discovery topic, and bootstrap PeerId from `relay-peer-id.json`.

## Overview

This example demonstrates how to create a [Yjs connection provider](https://docs.yjs.dev/ecosystem/connection-provider) using libp2p. The `yjs-libp2p-provider.js` file implements a custom provider that integrates libp2p's networking capabilities with Yjs, similar to how the standard [y-webrtc](https://github.com/yjs/y-webrtc) and [y-websocket](https://github.com/yjs/y-websocket) providers work, but with more control over the peer-to-peer networking stack.

Key features:

- **Custom Yjs Provider**: a libp2p-based connection provider for Yjs (`yjs-libp2p-provider.js`)
- **Circuit Relay**: NAT traversal via a relay server (implemented here in **Rust** libp2p)
- **AutoNAT / Identify**: peer identification + NAT detection
- **PubSub**: GossipSub for document synchronization
- **Peer Discovery**: automatic connection to discovered peers via pubsub peer discovery

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
       └────┤ Relay Node  │────┘
            │ (Rust)      │
            └─────────────┘
```

## Setup

### 1) Install frontend dependencies

```bash
npm install
```

### 2) Build and start the Rust relay server

```bash
cargo build --release
cargo run --release --bin relay
```

Or for development:

```bash
cargo run --bin relay
```

The relay loads `relay-peer-id.json` from the project root or `js-libp2p-example-yjs-libp2p/`. The PeerId must match `bootstrappers.js` for the frontend to connect.

The relay listens on:

- **TCP**: `/ip4/0.0.0.0/tcp/9091`
- **WebSocket**: `/ip4/0.0.0.0/tcp/9092/ws`
- **HTTP API**: `http://0.0.0.0:9094`  
  - `GET /api/addresses` → `{ websocket, webrtcDirect, tcp, all }` (CORS enabled)

### 3) Start the dev server

```bash
npm start
```

Open `http://localhost:5173` in multiple browser tabs/windows.

## Usage

1. Keep the default topic (`spreadsheet-1`) or enter a custom one
2. Click “Connect WebRTC-Direct” or “Connect WebSocket”
3. Open another tab/window and connect with the same topic
4. Changes will sync automatically between all connected peers

**Note:** The browser client fetches relay addresses from `http://localhost:9094/api/addresses`, and falls back to the static address in `bootstrappers.js`.

### Debug mode

**Rust relay:** set `RELAY_DEBUG=true` or `DEBUG=true` to log peer connect/disconnect and pubsub messages:

```bash
RELAY_DEBUG=true cargo run --bin relay
```

**Browser client:** add `?debug=true` to the URL:

```
http://localhost:5173/?debug=true
```

## Rust relay (port of `relay.js`)

`src/main.rs` implements the same behaviour as `js-libp2p-example-yjs-libp2p/relay.js`:

- Loads (or requires) `relay-peer-id.json`; PeerId must match `bootstrappers.js`
- Listens on TCP 9091 and WebSocket 9092
- Circuit relay server, GossipSub, identify, ping
- Subscribes to `_peer-discovery._p2p._pubsub` and default Yjs topics (`yjs-doc-1`, `spreadsheet-1`)
- HTTP server on 9094 with `/api/addresses` (CORS, same JSON shape as `relay.js`)
- Optional debug logging via `RELAY_DEBUG` / `DEBUG`

Constants (timeouts, discovery topic, etc.) align with `relay-constants.js` where applicable.

## Browser compatibility

This example has been tested with:

- ✅ **Chrome/Chromium**: fully supported and tested
- ✅ **Firefox**: fully supported and tested
- ⚠️ **Safari/WebKit**: partial support (WebRTC-Direct works better than WebSocket-based flows)

## How it works

### Libp2p configuration (browser)

The browser clients are configured with:

- **Transports**: WebSockets (for relay), WebRTC (for direct P2P), Circuit Relay
- **Security**: Noise protocol for encryption
- **Stream muxing**: Yamux
- **Services**:
  - `identify`: peer identification
  - `autoNAT`: NAT detection
  - `dcutr`: hole punching for direct connections
  - `pubsub`: GossipSub for broadcasting document updates

### Yjs integration

The custom `Libp2pProvider` class:

1. **Subscribes** to a pubsub topic for the Yjs document
2. **Listens** for Yjs document updates and broadcasts them via pubsub
3. **Receives** updates from other peers and applies them to the local document
4. **Discovers** peers subscribing to the same topic
5. **Connects** directly to discovered peers (using WebRTC when possible)
6. **Syncs** initial state using Yjs’s state vector protocol

### Message types

The provider uses three message types:

- `update`: broadcasts document changes to all peers
- `sync-request`: requests the current document state (sent on join)
- `sync-response`: sends the current state to a requesting peer

### Peer discovery flow

1. Client connects to relay server via WebRTC-Direct or WebSocket
2. Client subscribes to pubsub topics (peer discovery + document updates)
3. Relay forwards pubsub messages between peers
4. Peers attempt direct WebRTC connections (using DCUTR for NAT traversal)

## Need help?

- Read the [rust-libp2p documentation](https://docs.rs/libp2p)
- Check out the [general libp2p documentation](https://docs.libp2p.io)
- Read the [libp2p specs](https://github.com/libp2p/specs)
- Read the [Yjs documentation](https://docs.yjs.dev/)
