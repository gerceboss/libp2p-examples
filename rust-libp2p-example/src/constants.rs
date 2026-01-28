//! Relay configuration constants.

/// Debug mode – set RELAY_DEBUG=true or DEBUG=true
pub fn debug() -> bool {
    std::env::var("RELAY_DEBUG").as_deref() == Ok("true")
        || std::env::var("DEBUG").as_deref() == Ok("true")
}

/// Pubsub peer-discovery topic.
pub const DISCOVERY_TOPIC: &str = "_peer-discovery._p2p._pubsub";

/// Default Yjs document topics.
pub const DEFAULT_TOPICS: &[&str] = &["yjs-doc-1", "spreadsheet-1"];

/// Idle connection timeout (avoids closing browser connections during brief idle).
pub const IDLE_CONNECTION_TIMEOUT_SECS: u64 = 120;

/// HTTP API port.
pub const HTTP_PORT: u16 = 9094;

/// Bootstrap PeerId used by the frontend.
pub const EXPECTED_PEER_ID: &str = "12D3KooWP9ryj8o6uLRhUV2SXJycuBrynakzbiUMBmTn3prF8ezb";
