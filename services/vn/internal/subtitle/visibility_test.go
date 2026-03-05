package subtitle

import (
	"math"
	"testing"
)

func TestIsVisible(t *testing.T) {
	tests := []struct {
		name        string
		time        float64
		initVisible bool
		timestamps  []float64
		want        bool
	}{
		{
			name:        "always visible, no toggles",
			time:        5.0,
			initVisible: true,
			timestamps:  []float64{},
			want:        true,
		},
		{
			name:        "never visible, no toggles",
			time:        5.0,
			initVisible: false,
			timestamps:  []float64{},
			want:        false,
		},
		{
			name:        "appears at t=3, check before",
			time:        2.0,
			initVisible: false,
			timestamps:  []float64{3.0},
			want:        false,
		},
		{
			name:        "appears at t=3, check after",
			time:        4.0,
			initVisible: false,
			timestamps:  []float64{3.0},
			want:        true,
		},
		{
			name:        "appears at t=3, check exactly at toggle",
			time:        3.0,
			initVisible: false,
			timestamps:  []float64{3.0},
			want:        true,
		},
		{
			name:        "blink: visible 2-4, check inside",
			time:        3.0,
			initVisible: false,
			timestamps:  []float64{2.0, 4.0},
			want:        true,
		},
		{
			name:        "blink: visible 2-4, check after",
			time:        5.0,
			initVisible: false,
			timestamps:  []float64{2.0, 4.0},
			want:        false,
		},
		{
			name:        "starts visible, disappears at t=5",
			time:        6.0,
			initVisible: true,
			timestamps:  []float64{5.0},
			want:        false,
		},
		{
			name:        "starts visible, disappears at t=5, check before",
			time:        3.0,
			initVisible: true,
			timestamps:  []float64{5.0},
			want:        true,
		},
		{
			name:        "multiple toggles: on-off-on, check final on period",
			time:        8.0,
			initVisible: false,
			timestamps:  []float64{1.0, 3.0, 6.0},
			want:        true,
		},
		{
			name:        "multiple toggles: on-off-on, check middle off period",
			time:        4.0,
			initVisible: false,
			timestamps:  []float64{1.0, 3.0, 6.0},
			want:        false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := IsVisible(tt.time, tt.initVisible, tt.timestamps)
			if got != tt.want {
				t.Errorf("IsVisible(%v, %v, %v) = %v, want %v",
					tt.time, tt.initVisible, tt.timestamps, got, tt.want)
			}
		})
	}
}

func TestVisibilityWindows(t *testing.T) {
	tests := []struct {
		name        string
		initVisible bool
		timestamps  []float64
		duration    float64
		want        [][2]float64
	}{
		{
			name:        "always visible",
			initVisible: true,
			timestamps:  []float64{},
			duration:    10.0,
			want:        [][2]float64{{0, 10}},
		},
		{
			name:        "never visible",
			initVisible: false,
			timestamps:  []float64{},
			duration:    10.0,
			want:        nil,
		},
		{
			name:        "appears at 3, stays until end",
			initVisible: false,
			timestamps:  []float64{3.0},
			duration:    10.0,
			want:        [][2]float64{{3, 10}},
		},
		{
			name:        "blink 2-4",
			initVisible: false,
			timestamps:  []float64{2.0, 4.0},
			duration:    10.0,
			want:        [][2]float64{{2, 4}},
		},
		{
			name:        "starts visible, disappears at 5",
			initVisible: true,
			timestamps:  []float64{5.0},
			duration:    10.0,
			want:        [][2]float64{{0, 5}},
		},
		{
			name:        "on-off-on pattern",
			initVisible: true,
			timestamps:  []float64{2.0, 4.0, 8.0, 9.0},
			duration:    10.0,
			want:        [][2]float64{{0, 2}, {4, 8}, {9, 10}},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := VisibilityWindows(tt.initVisible, tt.timestamps, tt.duration)
			if len(got) != len(tt.want) {
				t.Fatalf("got %d windows, want %d: %v", len(got), len(tt.want), got)
			}
			for i := range got {
				if got[i] != tt.want[i] {
					t.Errorf("window[%d] = %v, want %v", i, got[i], tt.want[i])
				}
			}
		})
	}
}

func TestTotalVisibleDuration(t *testing.T) {
	// Blink 2-4 (2s), then on 6-10 (4s) = 6s total.
	dur := TotalVisibleDuration(false, []float64{2.0, 4.0, 6.0}, 10.0)
	if math.Abs(dur-6.0) > 0.001 {
		t.Errorf("total visible = %v, want 6.0", dur)
	}
}
