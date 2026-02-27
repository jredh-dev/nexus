// SPDX-License-Identifier: AGPL-3.0-or-later

use crate::bench;
use crate::tls::TlsConfig;

use std::sync::Arc;
use tokio::io::{AsyncReadExt, AsyncWriteExt};
use tokio::net::TcpListener;
use tokio_rustls::TlsAcceptor;
use tracing::{info, warn};

/// Raw TCP echo server for unencrypted latency benchmarking.
/// Protocol: client sends 8-byte payload, server prepends 16 bytes of timing
/// (server_recv_ns + server_send_ns as little-endian i64) and echoes back.
pub async fn serve_plaintext(port: u16) -> Result<(), Box<dyn std::error::Error + Send + Sync>> {
    let listener = TcpListener::bind(format!("0.0.0.0:{}", port)).await?;
    info!(port, "TCP echo server listening (plaintext)");

    loop {
        let (stream, addr) = listener.accept().await?;
        tokio::spawn(async move {
            if let Err(e) = handle_tcp_echo(stream).await {
                warn!(%addr, error = %e, "TCP echo connection error");
            }
        });
    }
}

/// TLS-encrypted TCP echo server for encrypted latency benchmarking.
pub async fn serve_tls(
    port: u16,
    tls_cfg: TlsConfig,
) -> Result<(), Box<dyn std::error::Error + Send + Sync>> {
    let listener = TcpListener::bind(format!("0.0.0.0:{}", port)).await?;
    let acceptor = TlsAcceptor::from(Arc::clone(&tls_cfg.server_config));
    info!(port, "TCP echo server listening (TLS)");

    loop {
        let (stream, addr) = listener.accept().await?;
        let acceptor = acceptor.clone();
        tokio::spawn(async move {
            match acceptor.accept(stream).await {
                Ok(tls_stream) => {
                    if let Err(e) = handle_tcp_echo(tls_stream).await {
                        warn!(%addr, error = %e, "TLS echo connection error");
                    }
                }
                Err(e) => {
                    warn!(%addr, error = %e, "TLS handshake failed");
                }
            }
        });
    }
}

/// Shared echo handler. Works with any AsyncRead+AsyncWrite.
/// Protocol:
///   Client sends: [len: u32 LE][payload: len bytes]
///   Server sends: [server_recv_ns: i64 LE][server_send_ns: i64 LE][payload: len bytes]
///   len == 0 means disconnect.
async fn handle_tcp_echo<S>(mut stream: S) -> Result<(), Box<dyn std::error::Error + Send + Sync>>
where
    S: AsyncReadExt + AsyncWriteExt + Unpin,
{
    let mut len_buf = [0u8; 4];
    loop {
        // Read payload length
        match stream.read_exact(&mut len_buf).await {
            Ok(_) => {}
            Err(e) if e.kind() == std::io::ErrorKind::UnexpectedEof => return Ok(()),
            Err(e) => return Err(e.into()),
        }
        let recv_ns = bench::now_ns();
        let len = u32::from_le_bytes(len_buf) as usize;
        if len == 0 {
            return Ok(());
        }

        // Read payload
        let mut payload = vec![0u8; len];
        stream.read_exact(&mut payload).await?;

        // Write response: timing + echo
        let send_ns = bench::now_ns();
        stream.write_all(&recv_ns.to_le_bytes()).await?;
        stream.write_all(&send_ns.to_le_bytes()).await?;
        stream.write_all(&payload).await?;
        stream.flush().await?;
    }
}
