// SPDX-License-Identifier: AGPL-3.0-or-later
// Copyright (c) 2026 Jared Redh. All rights reserved.

pub mod hermit {
    tonic::include_proto!("hermit");
}

mod grpc;
mod tcp;
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

    /// Raw TCP benchmark port (unencrypted)
    #[arg(long, default_value_t = 9091)]
    tcp_port: u16,

    /// TLS-encrypted TCP benchmark port
    #[arg(long, default_value_t = 9093)]
    tls_port: u16,

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
        tcp_port: args.tcp_port,
    });

    // Resolve TLS config (load from files or generate self-signed)
    let tls_cfg = tls::resolve_tls_config(args.tls_cert.as_deref(), args.tls_key.as_deref())?;

    info!(
        grpc_port = args.grpc_port,
        tcp_port = args.tcp_port,
        tls_port = args.tls_port,
        region = %args.region,
        "hermit-server starting"
    );

    // Spawn all listeners concurrently
    let grpc_handle = tokio::spawn(grpc::serve(args.grpc_port, server_state.clone(), tls_cfg.clone()));
    let tcp_handle = tokio::spawn(tcp::serve_plaintext(args.tcp_port));
    let tls_handle = tokio::spawn(tcp::serve_tls(args.tls_port, tls_cfg));

    // Wait for any to finish (they shouldn't unless error)
    tokio::select! {
        r = grpc_handle => {
            error!("gRPC server exited: {:?}", r);
        }
        r = tcp_handle => {
            error!("TCP server exited: {:?}", r);
        }
        r = tls_handle => {
            error!("TLS server exited: {:?}", r);
        }
    }

    Ok(())
}
