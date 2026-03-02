// SPDX-License-Identifier: AGPL-3.0-or-later

use crate::hermit::{
    hermit_server::{Hermit, HermitServer},
    BenchmarkRequest, BenchmarkResponse, DbStatsRequest, DbStatsResponse,
    KvGetRequest, KvGetResponse, KvListRequest, KvListResponse,
    KvSetRequest, KvSetResponse, LoginRequest, LoginResponse,
    PingRequest, PingResponse, ServerInfoRequest, ServerInfoResponse,
    SqlInsertRequest, SqlInsertResponse, SqlQueryRequest, SqlQueryResponse, SqlRow,
};
use crate::bench;
use crate::db::Database;
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
}

pub struct HermitService {
    state: Arc<ServerState>,
    tls_enabled: bool,
    db: Arc<Database>,
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
        }))
    }

    async fn kv_set(
        &self,
        req: Request<KvSetRequest>,
    ) -> Result<Response<KvSetResponse>, Status> {
        let inner = req.into_inner();
        match self.db.kv_set(inner.key, inner.value) {
            Ok(()) => Ok(Response::new(KvSetResponse {
                ok: true,
                error: String::new(),
            })),
            Err(e) => Ok(Response::new(KvSetResponse {
                ok: false,
                error: e,
            })),
        }
    }

    async fn kv_get(
        &self,
        req: Request<KvGetRequest>,
    ) -> Result<Response<KvGetResponse>, Status> {
        let inner = req.into_inner();
        match self.db.kv_get(&inner.key) {
            Ok(Some(value)) => Ok(Response::new(KvGetResponse {
                found: true,
                value,
                error: String::new(),
            })),
            Ok(None) => Ok(Response::new(KvGetResponse {
                found: false,
                value: Vec::new(),
                error: String::new(),
            })),
            Err(e) => Ok(Response::new(KvGetResponse {
                found: false,
                value: Vec::new(),
                error: e,
            })),
        }
    }

    async fn kv_list(
        &self,
        _req: Request<KvListRequest>,
    ) -> Result<Response<KvListResponse>, Status> {
        match self.db.kv_list() {
            Ok(keys) => Ok(Response::new(KvListResponse { keys })),
            Err(e) => Err(Status::internal(e)),
        }
    }

    async fn sql_insert(
        &self,
        req: Request<SqlInsertRequest>,
    ) -> Result<Response<SqlInsertResponse>, Status> {
        let inner = req.into_inner();
        match self.db.sql_insert(inner.key, inner.value) {
            Ok(_) => Ok(Response::new(SqlInsertResponse {
                queued: true,
                error: String::new(),
            })),
            Err(e) => Ok(Response::new(SqlInsertResponse {
                queued: false,
                error: e,
            })),
        }
    }

    async fn sql_query(
        &self,
        req: Request<SqlQueryRequest>,
    ) -> Result<Response<SqlQueryResponse>, Status> {
        let inner = req.into_inner();
        match self.db.sql_query(&inner.key_filter, inner.limit) {
            Ok(result) => {
                let rows = result
                    .rows
                    .into_iter()
                    .map(|r| SqlRow {
                        id: r.id,
                        key: r.key,
                        value: r.value,
                        created_at_unix: r.created_at_unix,
                    })
                    .collect();
                Ok(Response::new(SqlQueryResponse {
                    rows,
                    total_committed: result.total_committed,
                    pending_writes: result.pending_writes,
                }))
            }
            Err(e) => Err(Status::internal(e)),
        }
    }

    async fn db_stats(
        &self,
        _req: Request<DbStatsRequest>,
    ) -> Result<Response<DbStatsResponse>, Status> {
        let (doc_count, doc_bytes) = self.db.kv_stats().map_err(Status::internal)?;
        let (rel_rows, rel_pending) = self.db.rel_stats().map_err(Status::internal)?;
        Ok(Response::new(DbStatsResponse {
            doc_key_count: doc_count,
            doc_compressed_bytes: doc_bytes,
            rel_row_count: rel_rows,
            rel_pending_writes: rel_pending,
        }))
    }
}

pub async fn serve(
    port: u16,
    state: Arc<ServerState>,
    tls_cfg: TlsConfig,
    db: Arc<Database>,
) -> Result<(), Box<dyn std::error::Error + Send + Sync>> {
    let addr = format!("0.0.0.0:{}", port).parse()?;
    let svc = HermitService {
        state,
        tls_enabled: true,
        db,
    };

    let identity = tonic::transport::Identity::from_pem(&tls_cfg.cert_pem, &tls_cfg.key_pem);
    let tls = tonic::transport::ServerTlsConfig::new().identity(identity);

    info!(%addr, "gRPC server listening (TLS)");

    tonic::transport::Server::builder()
        .tls_config(tls)?
        .add_service(HermitServer::with_interceptor(svc, crate::auth::secret_interceptor))
        .serve(addr)
        .await?;

    Ok(())
}
