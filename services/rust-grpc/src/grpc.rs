// SPDX-License-Identifier: AGPL-3.0-or-later

use crate::hermit::{
    hermit_server::{Hermit, HermitServer},
    BenchmarkRequest, BenchmarkResponse, LoginRequest, LoginResponse,
    PingRequest, PingResponse, ServerInfoRequest, ServerInfoResponse,
};
use crate::bench;
use crate::tls::TlsConfig;

use prost_types::Timestamp;
use std::sync::Arc;
use std::time::{Instant, SystemTime, UNIX_EPOCH};
use tonic::{Request, Response, Status};
use tracing::info;

pub struct ServerState {
    pub version: String,
    pub region: String,
    pub started_at: SystemTime,
    pub start_instant: Instant,
    pub grpc_port: u16,
    pub tcp_port: u16,
}

pub struct HermitService {
    state: Arc<ServerState>,
    tls_enabled: bool,
}

#[tonic::async_trait]
impl Hermit for HermitService {
    async fn ping(&self, req: Request<PingRequest>) -> Result<Response<PingResponse>, Status> {
        let recv = bench::now_ns();
        let inner = req.into_inner();
        let send = bench::now_ns();
        Ok(Response::new(PingResponse {
            client_send_ns: inner.client_send_ns,
            server_recv_ns: recv,
            server_send_ns: send,
        }))
    }

    async fn benchmark(
        &self,
        req: Request<BenchmarkRequest>,
    ) -> Result<Response<BenchmarkResponse>, Status> {
        let inner = req.into_inner();
        let iterations = inner.iterations.max(1).min(10_000) as usize;
        let payload_bytes = inner.payload_bytes as usize;

        // Allocate payload once if needed (simulates processing)
        let _payload: Vec<u8> = if payload_bytes > 0 {
            vec![0xAB; payload_bytes]
        } else {
            Vec::new()
        };

        let mut latencies: Vec<i64> = Vec::with_capacity(iterations);
        let overhead_start = bench::now_ns();

        for _ in 0..iterations {
            let t0 = bench::now_ns();
            // Simulate minimal processing: touch the payload
            if payload_bytes > 0 {
                std::hint::black_box(&_payload);
            }
            let t1 = bench::now_ns();
            latencies.push(t1 - t0);
        }

        let overhead_end = bench::now_ns();
        latencies.sort_unstable();

        let stats = bench::Stats::from_sorted(&latencies);

        Ok(Response::new(BenchmarkResponse {
            latencies_ns: latencies,
            min_ns: stats.min,
            max_ns: stats.max,
            mean_ns: stats.mean,
            p50_ns: stats.p50,
            p99_ns: stats.p99,
            processing_overhead_ns: overhead_end - overhead_start,
            tls_active: self.tls_enabled,
            tls_version: if self.tls_enabled {
                "TLS 1.3".to_string()
            } else {
                String::new()
            },
        }))
    }

    async fn login(&self, req: Request<LoginRequest>) -> Result<Response<LoginResponse>, Status> {
        let inner = req.into_inner();
        info!(username = %inner.username, "login attempt (hardcoded success)");

        // TODO: Real auth. For now, always succeed.
        let session_id = uuid::Uuid::new_v4().to_string();
        Ok(Response::new(LoginResponse {
            success: true,
            session_id,
            error: String::new(),
        }))
    }

    async fn server_info(
        &self,
        _req: Request<ServerInfoRequest>,
    ) -> Result<Response<ServerInfoResponse>, Status> {
        let uptime = self.state.start_instant.elapsed().as_secs() as i64;
        let since_epoch = self.state.started_at
            .duration_since(UNIX_EPOCH)
            .unwrap_or_default();

        Ok(Response::new(ServerInfoResponse {
            version: self.state.version.clone(),
            region: self.state.region.clone(),
            started_at: Some(Timestamp {
                seconds: since_epoch.as_secs() as i64,
                nanos: since_epoch.subsec_nanos() as i32,
            }),
            uptime_seconds: uptime,
            rust_version: env!("CARGO_PKG_VERSION").to_string(),
            tls_enabled: self.tls_enabled,
            grpc_port: self.state.grpc_port as u32,
            tcp_port: self.state.tcp_port as u32,
        }))
    }
}

pub async fn serve(
    port: u16,
    state: Arc<ServerState>,
    tls_cfg: TlsConfig,
) -> Result<(), Box<dyn std::error::Error + Send + Sync>> {
    let addr = format!("0.0.0.0:{}", port).parse()?;
    let svc = HermitService {
        state,
        tls_enabled: true,
    };

    let identity = tonic::transport::Identity::from_pem(&tls_cfg.cert_pem, &tls_cfg.key_pem);
    let tls = tonic::transport::ServerTlsConfig::new().identity(identity);

    info!(%addr, "gRPC server listening (TLS)");

    tonic::transport::Server::builder()
        .tls_config(tls)?
        .add_service(HermitServer::new(svc))
        .serve(addr)
        .await?;

    Ok(())
}
