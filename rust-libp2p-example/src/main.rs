//! Rust libp2p relay server for Yjs collaborative spreadsheet.

use std::{
    collections::hash_map::DefaultHasher,
    hash::{Hash, Hasher},
    net::Ipv4Addr,
    path::PathBuf,
    sync::Arc,
    time::Duration,
};

use anyhow::{Context, Result};
use axum::{
    extract::State,
    http::Method,
    response::Json,
    routing::get,
    Router,
};
use base64::Engine;
use futures::StreamExt;
use libp2p::{
    core::multiaddr::Protocol,
    gossipsub::{self, IdentTopic, MessageAuthenticity, ValidationMode},
    identify, identity, noise, ping, relay,
    swarm::{NetworkBehaviour, SwarmEvent},
    tcp, yamux, Multiaddr, Swarm,
};
use tokio::sync::RwLock;
use tower_http::cors::{Any, CorsLayer};
use tracing_subscriber::EnvFilter;

/// Debug mode – set RELAY_DEBUG=true or DEBUG=true
fn debug() -> bool {
    std::env::var("RELAY_DEBUG").as_deref() == Ok("true")
        || std::env::var("DEBUG").as_deref() == Ok("true")
}

/// Pubsub peer-discovery topic.
const DISCOVERY_TOPIC: &str = "_peer-discovery._p2p._pubsub";

/// Default Yjs document topics.
const DEFAULT_TOPICS: &[&str] = &["yjs-doc-1", "spreadsheet-1"];

/// Idle connection timeout (avoids closing browser connections during brief idle).
const IDLE_CONNECTION_TIMEOUT_SECS: u64 = 120;

/// HTTP API port.
const HTTP_PORT: u16 = 9094;

/// Bootstrap PeerId used by the frontend.
const EXPECTED_PEER_ID: &str = "12D3KooWP9ryj8o6uLRhUV2SXJycuBrynakzbiUMBmTn3prF8ezb";

#[derive(Clone)]
struct AppState {
    peer_id: String,
    multiaddrs: Arc<RwLock<Vec<String>>>,
}

#[derive(NetworkBehaviour)]
struct Behaviour {
    relay: relay::Behaviour,
    ping: ping::Behaviour,
    identify: identify::Behaviour,
    gossipsub: gossipsub::Behaviour,
}

#[tokio::main]
async fn main() -> Result<()> {
    let _ = tracing_subscriber::fmt()
        .with_env_filter(EnvFilter::from_default_env().add_directive("libp2p=info".parse()?))
        .try_init();

    let keypair = load_keypair()?;
    let peer_id = keypair.public().to_peer_id();
    let peer_id_str = peer_id.to_string();
    if peer_id_str != EXPECTED_PEER_ID {
        anyhow::bail!(
            "PeerId mismatch: got {} expected {}",
            peer_id_str,
            EXPECTED_PEER_ID
        );
    }
    tracing::info!("Loaded existing PeerId: {}", peer_id_str);

    let mut swarm = build_swarm(keypair).await?;

    swarm.listen_on(
        Multiaddr::empty()
            .with(Protocol::Ip4(Ipv4Addr::UNSPECIFIED))
            .with(Protocol::Tcp(9091)),
    )?;
    swarm.listen_on(
        Multiaddr::empty()
            .with(Protocol::Ip4(Ipv4Addr::UNSPECIFIED))
            .with(Protocol::Tcp(9092))
            .with(Protocol::Ws("/".into())),
    )?;

    let multiaddrs = Arc::new(RwLock::new(Vec::new()));
    let state = AppState {
        peer_id: peer_id_str.clone(),
        multiaddrs: Arc::clone(&multiaddrs),
    };

    let app = Router::new()
        .route("/api/addresses", get(api_addresses))
        .layer(
            CorsLayer::new()
                .allow_origin(Any)
                .allow_methods([Method::GET, Method::OPTIONS])
                .allow_headers(Any),
        )
        .with_state(state);

    let listener = tokio::net::TcpListener::bind((std::net::Ipv4Addr::UNSPECIFIED, HTTP_PORT)).await?;
    let http_server = axum::serve(listener, app);

    tracing::info!("Subscribed to discovery topic: {}", DISCOVERY_TOPIC);
    let discover_topic = IdentTopic::new(DISCOVERY_TOPIC);
    swarm.behaviour_mut().gossipsub.subscribe(&discover_topic)?;

    for name in DEFAULT_TOPICS {
        let t = IdentTopic::new(*name);
        swarm.behaviour_mut().gossipsub.subscribe(&t)?;
        tracing::info!("Subscribed to topic: {}", name);
    }

    tracing::info!("\nHTTP API listening on: http://0.0.0.0:{}", HTTP_PORT);
    tracing::info!("Get addresses: http://localhost:{}/api/addresses\n", HTTP_PORT);

    let multiaddrs_clone = Arc::clone(&multiaddrs);
    let debug_mode = debug();
    tokio::spawn(async move {
        if let Err(e) = http_server.await {
            tracing::error!("HTTP server error: {}", e);
        }
    });

    let mut logged_listen = false;

    loop {
        let event = swarm.select_next_some().await;
        match &event {
            SwarmEvent::NewListenAddr { address, .. } => {
                let ma = address.clone().with(Protocol::P2p(peer_id));
                let s = ma.to_string();
                let local = s.replace("0.0.0.0", "127.0.0.1");
                if !logged_listen {
                    tracing::info!("Relay listening on:");
                    logged_listen = true;
                }
                tracing::info!("  {}", local);
                let mut addrs = multiaddrs_clone.write().await;
                if !addrs.contains(&local) {
                    addrs.push(local);
                }
            }
            SwarmEvent::ConnectionEstablished { peer_id: p, .. } if debug_mode => {
                let id = p.to_string();
                let short = peer_id_short(&id);
                tracing::info!("Peer connected: {}", short);
            }
            SwarmEvent::ConnectionClosed { peer_id: p, cause, .. } if debug_mode => {
                let id = p.to_string();
                let short = peer_id_short(&id);
                tracing::info!(
                    "Peer disconnected: {} cause={:?}",
                    short,
                    cause.as_ref().map(|c| c.to_string())
                );
            }
            SwarmEvent::Behaviour(ev) => {
                if let BehaviourEvent::Identify(identify::Event::Received {
                    info: identify::Info { observed_addr, .. },
                    ..
                }) = ev
                {
                    swarm.add_external_address(observed_addr.clone());
                }
                if debug_mode {
                    if let BehaviourEvent::Gossipsub(gossipsub::Event::Message {
                        propagation_source, ..
                    }) = ev
                    {
                        let short = peer_id_short(&propagation_source.to_string());
                        tracing::info!("Message on topic from {}", short);
                    }
                }
            }
            _ => {}
        }
    }
}

fn peer_id_short(id: &str) -> String {
    let n = id.len();
    if n <= 12 {
        id.to_string()
    } else {
        format!("{}...{}", &id[..8], &id[n - 4..])
    }
}

async fn api_addresses(State(state): State<AppState>) -> Json<serde_json::Value> {
    let addrs = state.multiaddrs.read().await;
    let all: Vec<String> = if addrs.is_empty() {
        vec![
            format!("/ip4/127.0.0.1/tcp/9091/p2p/{}", state.peer_id),
            format!("/ip4/127.0.0.1/tcp/9092/ws/p2p/{}", state.peer_id),
        ]
    } else {
        addrs.clone()
    };

    let websocket: Vec<String> = all.iter().filter(|ma| ma.contains("/ws")).cloned().collect();
    let webrtc_direct: Vec<String> = all.iter().filter(|ma| ma.contains("webrtc-direct")).cloned().collect();
    let tcp: Vec<String> = all
        .iter()
        .filter(|ma| ma.contains("/tcp/") && !ma.contains("/ws"))
        .cloned()
        .collect();

    let out = serde_json::json!({
        "websocket": websocket,
        "webrtcDirect": webrtc_direct,
        "tcp": tcp,
        "all": all,
    });
    Json(out)
}

fn load_keypair() -> Result<identity::Keypair> {
    let paths = [
        PathBuf::from("relay-peer-id.json"),
        PathBuf::from("js-libp2p-example-yjs-libp2p/relay-peer-id.json"),
    ];
    let path = paths
        .into_iter()
        .find(|p| p.exists())
        .context("relay-peer-id.json not found (tried ./ and js-libp2p-example-yjs-libp2p/)")?;

    let data = std::fs::read_to_string(&path).with_context(|| format!("read {}", path.display()))?;
    let json: serde_json::Value = serde_json::from_str(&data)?;
    let priv_b64 = json
        .get("privKey")
        .and_then(|v| v.as_str())
        .context("missing privKey")?;
    let mut bytes = base64::engine::general_purpose::STANDARD_NO_PAD
        .decode(priv_b64)
        .or_else(|_| base64::engine::general_purpose::STANDARD.decode(priv_b64))?;
    let keypair = identity::Keypair::from_protobuf_encoding(&mut bytes)
        .context("decode keypair from protobuf")?;
    Ok(keypair)
}

async fn build_swarm(keypair: identity::Keypair) -> Result<Swarm<Behaviour>> {
    let swarm = libp2p::SwarmBuilder::with_existing_identity(keypair.clone())
        .with_tokio()
        .with_tcp(
            tcp::Config::default(),
            noise::Config::new,
            yamux::Config::default,
        )?
        .with_websocket(noise::Config::new, yamux::Config::default)
        .await?
        .with_behaviour(|key| {
            let peer_id = key.public().to_peer_id();
            let message_id_fn = |message: &gossipsub::Message| {
                let mut s = DefaultHasher::new();
                message.data.hash(&mut s);
                gossipsub::MessageId::from(s.finish().to_string())
            };
            let gossipsub_config = gossipsub::ConfigBuilder::default()
                .heartbeat_interval(Duration::from_secs(2))
                .validation_mode(ValidationMode::Permissive)
                .message_id_fn(message_id_fn)
                .build()
                .map_err(|e| std::io::Error::other(format!("gossipsub config: {}", e)))?;

            let gossipsub = gossipsub::Behaviour::new(
                MessageAuthenticity::Signed(key.clone()),
                gossipsub_config,
            )
            .map_err(|e| std::io::Error::other(format!("gossipsub: {}", e)))?;

            let relay_config = relay::Config::default();
            Ok(Behaviour {
                relay: relay::Behaviour::new(peer_id, relay_config),
                ping: ping::Behaviour::new(ping::Config::new()),
                identify: identify::Behaviour::new(identify::Config::new(
                    "/yjs-libp2p-relay/0.1.0".to_string(),
                    key.public(),
                )),
                gossipsub,
            })
        })?
        .with_swarm_config(|c| {
            c.with_idle_connection_timeout(Duration::from_secs(IDLE_CONNECTION_TIMEOUT_SECS))
        })
        .build();

    Ok(swarm)
}
