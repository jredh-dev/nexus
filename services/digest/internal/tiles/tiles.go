// Package tiles defines the tile types for the digest service.
// A "tile" is a named metric cell displayed in the digest TUI and served via
// the /tiles HTTP endpoint.  Each tile holds a current value, an optional
// override, and the name of the reducer function used to compute it.
package tiles

import "time"

// Tile name constants — used as map keys and JSON identifiers throughout the
// digest service.
const (
	TileAvgLatency  = "avg_latency_ms"
	TilePublishRate = "publish_rate"
	TileErrorRate   = "error_rate"
	TileLastError   = "last_error"
	TileEventCount  = "event_count"
)

// AllTiles is the canonical ordered list of tile names.
// The consumer initialises one TileValue per entry.
var AllTiles = []string{
	TileAvgLatency,
	TilePublishRate,
	TileErrorRate,
	TileLastError,
	TileEventCount,
}

// TileValue holds either a computed or manually overridden value for a single
// tile.  Value is float64 for numeric tiles and string for text tiles.
type TileValue struct {
	Name       string    `json:"name"`
	Value      any       `json:"value"`          // float64 or string
	Overridden bool      `json:"overridden"`     // true if manually set via override record
	Func       string    `json:"func,omitempty"` // preferred reducer: avg/median/count/rate/last
	UpdatedAt  time.Time `json:"updated_at"`
}

// TileSnapshot is a full point-in-time snapshot of all tiles, published to
// the digest topic every tick and served from GET /tiles.
type TileSnapshot struct {
	Tiles      []TileValue `json:"tiles"`
	ComputedAt time.Time   `json:"computed_at"`
}

// OverrideRecord is published to the digest topic to manually override,
// reset, or change the reducer function of a tile.
//
//   - Type "override": set Tile to Value, mark as overridden
//   - Type "reset":    clear override flag, recompute on next tick
//   - Type "func":     store preferred Func name on tile (does not override)
type OverrideRecord struct {
	Type  string `json:"type"` // "override", "reset", "func"
	Tile  string `json:"tile"`
	Value any    `json:"value,omitempty"` // only for "override"
	Func  string `json:"func,omitempty"`  // only for "func"
}
