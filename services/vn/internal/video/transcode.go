package video

import (
	"context"
	"fmt"
)

// QualityTier defines a transcoding target.
type QualityTier struct {
	Label      string // e.g. "720p", "480p", "360p"
	Width      int    // output width (-1 for auto-scale)
	Height     int    // output height
	VideoBitrate string // e.g. "2000k"
	AudioBitrate string // e.g. "128k"
}

// DefaultTiers returns the standard quality tiers for adaptive streaming.
func DefaultTiers() []QualityTier {
	return []QualityTier{
		{Label: "720p", Width: -2, Height: 720, VideoBitrate: "2000k", AudioBitrate: "128k"},
		{Label: "480p", Width: -2, Height: 480, VideoBitrate: "1000k", AudioBitrate: "96k"},
		{Label: "360p", Width: -2, Height: 360, VideoBitrate: "500k", AudioBitrate: "64k"},
	}
}

// Transcode converts inputPath to the specified quality tier, writing
// the result to outPath. Uses libx264 + AAC for maximum compatibility.
func Transcode(ctx context.Context, inputPath, outPath string, tier QualityTier) error {
	scaleFilter := fmt.Sprintf("scale=%d:%d", tier.Width, tier.Height)

	return runFFmpeg(ctx,
		"-i", inputPath,
		"-vf", scaleFilter,
		"-c:v", "libx264", "-preset", "fast", "-b:v", tier.VideoBitrate,
		"-c:a", "aac", "-b:a", tier.AudioBitrate,
		"-movflags", "+faststart", // Move moov atom for streaming.
		"-y", outPath,
	)
}

// TranscodeAll creates all default quality tiers from inputPath.
// Output files are written to outDir as "{label}.mp4".
// Returns the paths of all generated files.
func TranscodeAll(ctx context.Context, inputPath, outDir string) ([]string, error) {
	var paths []string
	for _, tier := range DefaultTiers() {
		outPath := fmt.Sprintf("%s/%s.mp4", outDir, tier.Label)
		if err := Transcode(ctx, inputPath, outPath, tier); err != nil {
			return paths, fmt.Errorf("transcode %s: %w", tier.Label, err)
		}
		paths = append(paths, outPath)
	}
	return paths, nil
}
