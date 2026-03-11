// SPDX-License-Identifier: AGPL-3.0-or-later
package tui

import tea "charm.land/bubbletea/v2"

// IsQuit returns true if the key is a standard quit binding: q, esc, ctrl+c.
func IsQuit(k tea.Key) bool {
	return k.Code == 'q' ||
		k.Code == tea.KeyEscape ||
		(k.Code == 'c' && k.Mod == tea.ModCtrl)
}
