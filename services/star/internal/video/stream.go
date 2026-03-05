package video

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"strconv"

	"github.com/google/uuid"
	"github.com/jredh-dev/nexus/services/star/internal/database"
)

// StreamHandler returns an http.HandlerFunc that streams video data from
// PostgreSQL large objects with support for HTTP Range requests.
//
// URL pattern: GET /api/video/{id}
// Headers: Range (optional), Accept
// Response: 200 (full) or 206 (partial) with Content-Type from video metadata.
func StreamHandler(db *database.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		idStr := r.PathValue("id")
		if idStr == "" {
			http.Error(w, "missing video id", http.StatusBadRequest)
			return
		}

		id, err := uuid.Parse(idStr)
		if err != nil {
			http.Error(w, "invalid video id", http.StatusBadRequest)
			return
		}

		v, err := db.GetVideo(r.Context(), id)
		if err != nil {
			http.Error(w, "video not found", http.StatusNotFound)
			return
		}

		// For range requests, we need to handle seek within the large object.
		// Since pgx LargeObject implements io.ReadSeeker, we can use
		// http.ServeContent — but it requires an io.ReadSeeker, so we
		// read into the response manually for large objects.
		rangeHeader := r.Header.Get("Range")

		err = db.ReadVideoData(r.Context(), id, func(reader io.Reader, sizeBytes int64) error {
			w.Header().Set("Content-Type", v.MimeType)
			w.Header().Set("Accept-Ranges", "bytes")

			if rangeHeader == "" {
				// Full response.
				w.Header().Set("Content-Length", strconv.FormatInt(sizeBytes, 10))
				w.WriteHeader(http.StatusOK)
				_, err := io.Copy(w, reader)
				return err
			}

			// Parse Range header: "bytes=start-end"
			start, end, err := parseRange(rangeHeader, sizeBytes)
			if err != nil {
				http.Error(w, "invalid range", http.StatusRequestedRangeNotSatisfiable)
				return nil // Error already written.
			}

			// Seek to start if reader supports it.
			if seeker, ok := reader.(io.ReadSeeker); ok {
				if _, err := seeker.Seek(start, io.SeekStart); err != nil {
					return fmt.Errorf("seek: %w", err)
				}
			} else {
				// Fallback: discard bytes up to start.
				if _, err := io.CopyN(io.Discard, reader, start); err != nil {
					return fmt.Errorf("skip to range start: %w", err)
				}
			}

			length := end - start + 1
			w.Header().Set("Content-Length", strconv.FormatInt(length, 10))
			w.Header().Set("Content-Range",
				fmt.Sprintf("bytes %d-%d/%d", start, end, sizeBytes))
			w.WriteHeader(http.StatusPartialContent)
			_, err = io.CopyN(w, reader, length)
			return err
		})
		if err != nil {
			// Log but don't send another error — headers may already be sent.
			fmt.Fprintf(io.Discard, "stream error: %v", err)
		}
	}
}

// parseRange extracts start and end byte positions from a Range header.
// Returns (start, end, error). End is inclusive.
func parseRange(rangeHeader string, totalSize int64) (int64, int64, error) {
	// Expected format: "bytes=start-end" or "bytes=start-"
	if len(rangeHeader) < 6 || rangeHeader[:6] != "bytes=" {
		return 0, 0, fmt.Errorf("unsupported range format")
	}
	rangeSpec := rangeHeader[6:]

	var start, end int64

	dashIdx := -1
	for i, c := range rangeSpec {
		if c == '-' {
			dashIdx = i
			break
		}
	}
	if dashIdx < 0 {
		return 0, 0, fmt.Errorf("missing dash in range")
	}

	startStr := rangeSpec[:dashIdx]
	endStr := rangeSpec[dashIdx+1:]

	if startStr == "" {
		// Suffix range: "-500" means last 500 bytes.
		n, err := strconv.ParseInt(endStr, 10, 64)
		if err != nil {
			return 0, 0, err
		}
		start = totalSize - n
		end = totalSize - 1
	} else {
		var err error
		start, err = strconv.ParseInt(startStr, 10, 64)
		if err != nil {
			return 0, 0, err
		}
		if endStr == "" {
			end = totalSize - 1
		} else {
			end, err = strconv.ParseInt(endStr, 10, 64)
			if err != nil {
				return 0, 0, err
			}
		}
	}

	if start < 0 || start >= totalSize || end >= totalSize || start > end {
		return 0, 0, fmt.Errorf("range out of bounds")
	}

	return start, end, nil
}

// contextKey is unexported to avoid collisions with other packages.
type contextKey string

// VideoIDKey is the context key for the video UUID in route handlers.
const VideoIDKey contextKey = "videoID"

// SetVideoID adds the video UUID to the request context.
func SetVideoID(ctx context.Context, id uuid.UUID) context.Context {
	return context.WithValue(ctx, VideoIDKey, id)
}
