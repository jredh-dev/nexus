// Package subtitle implements the toggle-point visibility model for
// text overlays on video.
//
// The model works like a flip-flop switch:
//   - initialize_visible determines the state at time 0
//   - Each timestamp in the array toggles visibility
//   - end_visible must match the final state after all toggles
//
// This package provides both the Go runtime calculator (for server-side
// validation and subtitle timing) and validation logic that mirrors the
// PostgreSQL CHECK constraint.
package subtitle

import "sort"

// IsVisible returns whether a subtitle should be shown at the given
// playback time (in seconds).
//
// Parameters:
//   - t: current playback time in seconds
//   - initVisible: whether the subtitle is visible at t=0
//   - timestamps: sorted toggle points in seconds
//
// The function counts how many toggle points have been passed and
// determines the current state by flipping from the initial state.
func IsVisible(t float64, initVisible bool, timestamps []float64) bool {
	// Count toggles that have occurred by time t.
	toggles := 0
	for _, ts := range timestamps {
		if ts <= t {
			toggles++
		} else {
			break // timestamps are sorted, no need to continue.
		}
	}

	// Even number of toggles = same as initial state.
	// Odd number of toggles = opposite of initial state.
	if toggles%2 == 0 {
		return initVisible
	}
	return !initVisible
}

// VisibilityWindows returns the time ranges during which the subtitle
// is visible. Each window is a [start, end) pair. The final window
// may have end = -1 to indicate "until end of video."
//
// This is useful for rendering subtitle tracks or debugging.
func VisibilityWindows(initVisible bool, timestamps []float64, videoDuration float64) [][2]float64 {
	var windows [][2]float64
	visible := initVisible
	lastTime := 0.0

	sorted := make([]float64, len(timestamps))
	copy(sorted, timestamps)
	sort.Float64s(sorted)

	for _, t := range sorted {
		if visible {
			windows = append(windows, [2]float64{lastTime, t})
		}
		visible = !visible
		lastTime = t
	}

	// If still visible at the end, add final window.
	if visible {
		windows = append(windows, [2]float64{lastTime, videoDuration})
	}

	return windows
}

// TotalVisibleDuration returns the total seconds the subtitle is visible.
func TotalVisibleDuration(initVisible bool, timestamps []float64, videoDuration float64) float64 {
	windows := VisibilityWindows(initVisible, timestamps, videoDuration)
	var total float64
	for _, w := range windows {
		total += w[1] - w[0]
	}
	return total
}
