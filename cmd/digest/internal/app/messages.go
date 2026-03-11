// SPDX-License-Identifier: AGPL-3.0-or-later
package app

import (
	"github.com/jredh-dev/nexus/internal/digest/tiles"
	tuitypes "github.com/jredh-dev/nexus/internal/tui"
)

// MsgTileSnapshot is sent by the Kafka consumer goroutine whenever a new
// TileSnapshot arrives on the digest topic.
type MsgTileSnapshot struct{ Snap tiles.TileSnapshot }

// MsgKafkaError is sent when the Kafka consumer encounters a fatal read error.
// Uses the shared type from internal/tui.
type MsgKafkaError = tuitypes.MsgKafkaError

// MsgPublishDone is sent after a CLI-style publish (set/reset/apply) completes.
// Err is nil on success.
type MsgPublishDone struct{ Err error }
