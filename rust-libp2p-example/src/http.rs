//! HTTP API server for /api/addresses.

use std::sync::Arc;

use axum::{
    extract::State,
    http::Method,
    response::Json,
    routing::get,
    Router,
};
use tokio::sync::RwLock;
use tower_http::cors::{Any, CorsLayer};

#[derive(Clone)]
pub struct AppState {
    pub peer_id: String,
    pub multiaddrs: Arc<RwLock<Vec<String>>>,
}

pub fn router(state: AppState) -> Router {
    Router::new()
        .route("/api/addresses", get(api_addresses))
        .layer(
            CorsLayer::new()
                .allow_origin(Any)
                .allow_methods([Method::GET, Method::OPTIONS])
                .allow_headers(Any),
        )
        .with_state(state)
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
