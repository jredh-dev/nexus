// SPDX-License-Identifier: AGPL-3.0-or-later

use rcgen::{generate_simple_self_signed, CertifiedKey};
use rustls::ServerConfig;
use rustls_pemfile::{certs, pkcs8_private_keys};
use std::io::BufReader;
use std::sync::Arc;
use tracing::info;

#[derive(Clone)]
pub struct TlsConfig {
    pub cert_pem: Vec<u8>,
    pub key_pem: Vec<u8>,
    pub server_config: Arc<ServerConfig>,
}

/// Load TLS from files or generate self-signed cert for development.
pub fn resolve_tls_config(
    cert_path: Option<&str>,
    key_path: Option<&str>,
) -> Result<TlsConfig, Box<dyn std::error::Error>> {
    let (cert_pem, key_pem) = match (cert_path, key_path) {
        (Some(c), Some(k)) => {
            info!("loading TLS cert from {}, key from {}", c, k);
            (std::fs::read(c)?, std::fs::read(k)?)
        }
        _ => {
            info!("generating self-signed TLS certificate");
            let CertifiedKey { cert, key_pair } = generate_simple_self_signed(vec![
                "localhost".to_string(),
                "hermit.local".to_string(),
                "127.0.0.1".to_string(),
            ])?;
            (
                cert.pem().as_bytes().to_vec(),
                key_pair.serialize_pem().as_bytes().to_vec(),
            )
        }
    };

    let server_config = build_rustls_config(&cert_pem, &key_pem)?;

    Ok(TlsConfig {
        cert_pem,
        key_pem,
        server_config: Arc::new(server_config),
    })
}

fn build_rustls_config(
    cert_pem: &[u8],
    key_pem: &[u8],
) -> Result<ServerConfig, Box<dyn std::error::Error>> {
    let cert_chain = certs(&mut BufReader::new(cert_pem)).collect::<Result<Vec<_>, _>>()?;
    let mut keys =
        pkcs8_private_keys(&mut BufReader::new(key_pem)).collect::<Result<Vec<_>, _>>()?;

    if keys.is_empty() {
        return Err("no private keys found in PEM".into());
    }

    let config = ServerConfig::builder()
        .with_no_client_auth()
        .with_single_cert(cert_chain, keys.remove(0).into())?;

    Ok(config)
}
