//! Libp2p relay: swarm, keypair, topic subscription, event loop.

use std::{
    collections::hash_map::DefaultHasher,
    hash::{Hash, Hasher},
    path::PathBuf,
    sync::Arc,
    time::Duration,
};

use anyhow::{Context, Result};
use base64::Engine;
use futures::StreamExt;
use libp2p::{
    core::multiaddr::Protocol,
    gossipsub::{self, IdentTopic, MessageAuthenticity, ValidationMode},
    identify, identity, noise, ping, relay,
    swarm::{NetworkBehaviour, SwarmEvent},
    tcp, yamux, Swarm,
};
use tokio::sync::RwLock;

use crate::constants::{self, DEFAULT_TOPICS, DISCOVERY_TOPIC};

#[derive(NetworkBehaviour)]
pub struct Behaviour {
    relay: relay::Behaviour,
    ping: ping::Behaviour,
    identify: identify::Behaviour,
    gossipsub: gossipsub::Behaviour,
}

pub fn load_keypair() -> Result<identity::Keypair> {
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

pub async fn build_swarm(keypair: identity::Keypair) -> Result<Swarm<Behaviour>> {
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
            c.with_idle_connection_timeout(Duration::from_secs(
                constants::IDLE_CONNECTION_TIMEOUT_SECS,
            ))
        })
        .build();

    Ok(swarm)
}

pub fn subscribe_topics(swarm: &mut Swarm<Behaviour>) -> Result<()> {
    let discover_topic = IdentTopic::new(DISCOVERY_TOPIC);
    swarm.behaviour_mut().gossipsub.subscribe(&discover_topic)?;
    tracing::info!("Subscribed to discovery topic: {}", DISCOVERY_TOPIC);

    for name in DEFAULT_TOPICS {
        let t = IdentTopic::new(*name);
        swarm.behaviour_mut().gossipsub.subscribe(&t)?;
        tracing::info!("Subscribed to topic: {}", name);
    }
    Ok(())
}

pub async fn run_loop(
    swarm: &mut Swarm<Behaviour>,
    multiaddrs: Arc<RwLock<Vec<String>>>,
    peer_id: libp2p::PeerId,
) -> ! {
    let debug_mode = constants::debug();
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
                let mut addrs = multiaddrs.write().await;
                if !addrs.contains(&local) {
                    addrs.push(local);
                }
            }
            SwarmEvent::ConnectionEstablished { peer_id: p, .. } if debug_mode => {
                tracing::info!("Peer connected: {}", peer_id_short(&p.to_string()));
            }
            SwarmEvent::ConnectionClosed { peer_id: p, cause, .. } if debug_mode => {
                tracing::info!(
                    "Peer disconnected: {} cause={:?}",
                    peer_id_short(&p.to_string()),
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
                        propagation_source,
                        ..
                    }) = ev
                    {
                        tracing::info!(
                            "Message on topic from {}",
                            peer_id_short(&propagation_source.to_string())
                        );
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
