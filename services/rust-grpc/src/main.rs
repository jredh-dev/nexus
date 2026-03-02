// SPDX-License-Identifier: AGPL-3.0-or-later
// Copyright (c) 2026 Jared Redh. All rights reserved.

pub mod hermit {
    tonic::include_proto!("hermit");
}

mod grpc;
mod tls;
mod bench;

use clap::Parser;
use std::sync::Arc;
use tracing::{info, error};

#[derive(Parser, Debug)]
#[command(name = "hermit-server", version, about = "Hermit high-performance server")]
struct Args {
    /// gRPC listen port
    #[arg(long, default_value_t = 9090)]
    grpc_port: u16,

    /// Region identifier for ServerInfo
    #[arg(long, default_value = "us-west1")]
    region: String,

    /// Path to TLS certificate (PEM). Auto-generates self-signed if absent.
    #[arg(long)]
    tls_cert: Option<String>,

    /// Path to TLS private key (PEM). Auto-generates self-signed if absent.
    #[arg(long)]
    tls_key: Option<String>,
}

#[tokio::main]
async fn main() -> Result<(), Box<dyn std::error::Error>> {
    // Install ring as the default crypto provider for rustls
    rustls::crypto::ring::default_provider()
        .install_default()
        .expect("failed to install rustls crypto provider");

    tracing_subscriber::fmt()
        .with_env_filter(
            tracing_subscriber::EnvFilter::try_from_default_env()
                .unwrap_or_else(|_| "hermit_server=info,tower=warn".into()),
        )
        .init();

    let args = Args::parse();
    let start_time = std::time::Instant::now();
    let started_at = std::time::SystemTime::now();

    let server_state = Arc::new(grpc::ServerState {
        version: env!("CARGO_PKG_VERSION").to_string(),
        region: args.region.clone(),
        started_at,
        start_instant: start_time,
        grpc_port: args.grpc_port,
    });

    // Resolve TLS config (load from files or generate self-signed)
    let tls_cfg = tls::resolve_tls_config(args.tls_cert.as_deref(), args.tls_key.as_deref())?;

    info!(
        grpc_port = args.grpc_port,
        region = %args.region,
        "hermit-server starting"
    );

    // Run gRPC server (only listener for Cloud Run single-port)
    if let Err(e) = grpc::serve(args.grpc_port, server_state, tls_cfg).await {
        error!("gRPC server exited with error: {:?}", e);
    }

    Ok(())
}
