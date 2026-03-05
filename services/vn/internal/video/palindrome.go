// Package video provides FFmpeg-based video processing for the vn engine.
//
// Key operations:
//   - Palindrome loop generation (forward + reverse concatenation)
//   - Adaptive quality transcoding (multiple resolution/bitrate tiers)
//   - HTTP range-request streaming from PostgreSQL large objects
package video

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
)

// FFmpegPath is the path to the ffmpeg binary. Override for testing or
// non-standard installs.
var FFmpegPath = "ffmpeg"

// init tries common Homebrew locations if ffmpeg isn't on PATH.
func init() {
	if _, err := exec.LookPath(FFmpegPath); err != nil {
		for _, p := range []string{"/usr/local/bin/ffmpeg", "/opt/homebrew/bin/ffmpeg"} {
			if _, err := os.Stat(p); err == nil {
				FFmpegPath = p
				break
			}
		}
	}
}

// GeneratePalindrome creates a "racecar loop" video: the input played
// forward then reversed, so the last frame matches the first frame for
// seamless looping.
//
// The output is written to outPath. Codec and container are preserved
// from the input by default (re-encoded with libx264 for reliability).
//
// Steps:
//  1. Reverse the input (video + audio) to a temp file
//  2. Concatenate original + reversed via the concat demuxer
//  3. Clean up temp files
func GeneratePalindrome(ctx context.Context, inputPath, outPath string) error {
	dir, err := os.MkdirTemp("", "vn-palindrome-*")
	if err != nil {
		return fmt.Errorf("create temp dir: %w", err)
	}
	defer os.RemoveAll(dir)

	reversedPath := filepath.Join(dir, "reversed.mp4")
	concatList := filepath.Join(dir, "concat.txt")

	// Step 1: Reverse.
	err = runFFmpeg(ctx,
		"-i", inputPath,
		"-vf", "reverse",
		"-af", "areverse",
		"-c:v", "libx264", "-preset", "fast", "-crf", "18",
		"-c:a", "aac",
		"-y", reversedPath,
	)
	if err != nil {
		return fmt.Errorf("reverse video: %w", err)
	}

	// Step 2: Write concat list.
	absInput, err := filepath.Abs(inputPath)
	if err != nil {
		return fmt.Errorf("abs path: %w", err)
	}
	listContent := fmt.Sprintf("file '%s'\nfile '%s'\n", absInput, reversedPath)
	if err := os.WriteFile(concatList, []byte(listContent), 0644); err != nil {
		return fmt.Errorf("write concat list: %w", err)
	}

	// Step 3: Concatenate.
	err = runFFmpeg(ctx,
		"-f", "concat", "-safe", "0",
		"-i", concatList,
		"-c", "copy",
		"-y", outPath,
	)
	if err != nil {
		return fmt.Errorf("concatenate palindrome: %w", err)
	}

	return nil
}

// runFFmpeg executes ffmpeg with the given arguments.
func runFFmpeg(ctx context.Context, args ...string) error {
	cmd := exec.CommandContext(ctx, FFmpegPath, args...)
	cmd.Stdout = os.Stderr // FFmpeg logs to stderr by default; capture both.
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("ffmpeg %v: %w", args[:2], err)
	}
	return nil
}
