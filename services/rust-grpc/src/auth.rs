// SPDX-License-Identifier: AGPL-3.0-or-later

use tonic::{Request, Status};
use tracing::warn;

/// Extracts the shared secret from environment and validates it against
/// the `x-hermit-secret` metadata header on each gRPC request.
///
/// If HERMIT_SECRET is not set (dev mode), all requests are allowed.
pub fn secret_interceptor(req: Request<()>) -> Result<Request<()>, Status> {
    let expected = match std::env::var("HERMIT_SECRET") {
        Ok(s) if !s.is_empty() => s,
        _ => return Ok(req), // dev mode: no secret required
    };

    match req.metadata().get("x-hermit-secret") {
        Some(val) => match val.to_str() {
            Ok(v) if v == expected => Ok(req),
            _ => {
                warn!("invalid x-hermit-secret");
                Err(Status::unauthenticated("invalid secret"))
            }
        },
        None => {
            warn!("missing x-hermit-secret header");
            Err(Status::unauthenticated("missing secret"))
        }
    }
}
