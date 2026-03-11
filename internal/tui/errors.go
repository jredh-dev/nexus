// SPDX-License-Identifier: AGPL-3.0-or-later
package tui

import "fmt"

// MsgKafkaError is the shared Kafka consumer error message type.
// Both realtime and digest should use this instead of defining their own.
type MsgKafkaError struct{ Err error }

// RenderError renders err as a styled error line, or "" if err is nil.
func RenderError(err error) string {
	if err == nil {
		return ""
	}
	return ErrStyle.Render(fmt.Sprintf("error: %v", err))
}
