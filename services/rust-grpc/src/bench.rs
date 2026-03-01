// SPDX-License-Identifier: AGPL-3.0-or-later

use std::time::Instant;

/// Returns current monotonic time in nanoseconds.
/// Uses `Instant` which is backed by clock_gettime(CLOCK_MONOTONIC) on Linux
/// and mach_absolute_time on macOS -- sub-microsecond resolution.
#[inline(always)]
pub fn now_ns() -> i64 {
    // We use a process-local epoch to keep values small and avoid overflow.
    static EPOCH: std::sync::OnceLock<Instant> = std::sync::OnceLock::new();
    let epoch = EPOCH.get_or_init(Instant::now);
    epoch.elapsed().as_nanos() as i64
}

pub struct Stats {
    pub min: i64,
    pub max: i64,
    pub mean: i64,
    pub p50: i64,
    pub p99: i64,
}

impl Stats {
    /// Compute stats from a pre-sorted slice of latency values.
    pub fn from_sorted(sorted: &[i64]) -> Self {
        if sorted.is_empty() {
            return Stats {
                min: 0,
                max: 0,
                mean: 0,
                p50: 0,
                p99: 0,
            };
        }
        let n = sorted.len();
        let sum: i64 = sorted.iter().sum();
        Stats {
            min: sorted[0],
            max: sorted[n - 1],
            mean: sum / n as i64,
            p50: sorted[n / 2],
            p99: sorted[(n as f64 * 0.99) as usize],
        }
    }
}
