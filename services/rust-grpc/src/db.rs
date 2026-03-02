// SPDX-License-Identifier: AGPL-3.0-or-later

use std::collections::HashMap;
use std::sync::RwLock;
use std::time::{SystemTime, UNIX_EPOCH};

/// In-memory document + relational database for hermit.
/// Thread-safe via RwLock. No persistence -- data lives for server lifetime.
pub struct Database {
    docs: RwLock<HashMap<String, Vec<u8>>>,
    rows: RwLock<RelStore>,
}

struct RelStore {
    committed: Vec<Row>,
    pending: Vec<Row>,
    next_id: u64,
}

#[derive(Clone)]
pub struct Row {
    pub id: u64,
    pub key: String,
    pub value: String,
    pub created_at_unix: i64,
}

pub struct QueryResult {
    pub rows: Vec<Row>,
    pub total_committed: u64,
    pub pending_writes: u64,
}

impl Database {
    pub fn new() -> Self {
        Database {
            docs: RwLock::new(HashMap::new()),
            rows: RwLock::new(RelStore {
                committed: Vec::new(),
                pending: Vec::new(),
                next_id: 1,
            }),
        }
    }

    // --- Document store ---

    pub fn kv_set(&self, key: String, value: Vec<u8>) -> Result<(), String> {
        let mut docs = self.docs.write().map_err(|e| e.to_string())?;
        docs.insert(key, value);
        Ok(())
    }

    pub fn kv_get(&self, key: &str) -> Result<Option<Vec<u8>>, String> {
        let docs = self.docs.read().map_err(|e| e.to_string())?;
        Ok(docs.get(key).cloned())
    }

    pub fn kv_list(&self) -> Result<Vec<String>, String> {
        let docs = self.docs.read().map_err(|e| e.to_string())?;
        let mut keys: Vec<String> = docs.keys().cloned().collect();
        keys.sort();
        Ok(keys)
    }

    pub fn kv_stats(&self) -> Result<(u64, u64), String> {
        let docs = self.docs.read().map_err(|e| e.to_string())?;
        let count = docs.len() as u64;
        let bytes: u64 = docs.values().map(|v| v.len() as u64).sum();
        Ok((count, bytes))
    }

    // --- Relational store ---

    pub fn sql_insert(&self, key: String, value: String) -> Result<bool, String> {
        let mut store = self.rows.write().map_err(|e| e.to_string())?;
        let now = SystemTime::now()
            .duration_since(UNIX_EPOCH)
            .unwrap_or_default()
            .as_secs() as i64;
        let row = Row {
            id: store.next_id,
            key,
            value,
            created_at_unix: now,
        };
        store.next_id += 1;
        store.committed.push(row);
        Ok(true)
    }

    pub fn sql_query(&self, key_filter: &str, limit: u32) -> Result<QueryResult, String> {
        let store = self.rows.read().map_err(|e| e.to_string())?;
        let limit = if limit == 0 { 100 } else { limit as usize };

        let rows: Vec<Row> = store
            .committed
            .iter()
            .filter(|r| key_filter.is_empty() || r.key.contains(key_filter))
            .take(limit)
            .cloned()
            .collect();

        Ok(QueryResult {
            rows,
            total_committed: store.committed.len() as u64,
            pending_writes: store.pending.len() as u64,
        })
    }

    pub fn rel_stats(&self) -> Result<(u64, u64), String> {
        let store = self.rows.read().map_err(|e| e.to_string())?;
        Ok((store.committed.len() as u64, store.pending.len() as u64))
    }
}
