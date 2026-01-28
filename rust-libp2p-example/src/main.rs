//! Rust libp2p relay server for Yjs collaborative spreadsheet.

mod constants;
mod http;
mod relay;

use std::net::Ipv4Addr;
use std::sync::Arc;

use anyhow::Result;
use libp2p::core::multiaddr::Protocol;
use libp2p::Multiaddr;
use tokio::sync::RwLock;
use tracing_subscriber::EnvFilter;

use constants::{EXPECTED_PEER_ID, HTTP_PORT};
use http::{router, AppState};
use relay::{build_swarm, load_keypair, run_loop, subscribe_topics};

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

    let app = router(state);
    let listener = tokio::net::TcpListener::bind((std::net::Ipv4Addr::UNSPECIFIED, HTTP_PORT)).await?;
    let http_server = axum::serve(listener, app);

    subscribe_topics(&mut swarm)?;

    tracing::info!("\nHTTP API listening on: http://0.0.0.0:{}", HTTP_PORT);
    tracing::info!("Get addresses: http://localhost:{}/api/addresses\n", HTTP_PORT);

    let multiaddrs_clone = Arc::clone(&multiaddrs);
    tokio::spawn(async move {
        if let Err(e) = http_server.await {
            tracing::error!("HTTP server error: {}", e);
        }
    });

    run_loop(&mut swarm, multiaddrs_clone, peer_id).await
}
