// starctl is the CLI tool for the star visual novel engine.
//
// Usage:
//
//	starctl video import <file> [--name NAME] [--codec CODEC] [--duration MS]
//	starctl video palindrome <video-id> [--output FILE]
//	starctl video list
//	starctl story validate <dir>
//	starctl story compile <dir> --output manifest.json
package main

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"strconv"

	"github.com/jredh-dev/nexus/services/star/internal/database"
	"github.com/jredh-dev/nexus/services/star/internal/video"
)

const defaultConnStr = "host=/tmp/ctl-pg dbname=star user=jredh"

func main() {
	if len(os.Args) < 2 {
		usage()
		os.Exit(1)
	}

	switch os.Args[1] {
	case "video":
		if len(os.Args) < 3 {
			usage()
			os.Exit(1)
		}
		switch os.Args[2] {
		case "import":
			cmdVideoImport(os.Args[3:])
		case "palindrome":
			cmdVideoPalindrome(os.Args[3:])
		case "list":
			cmdVideoList()
		default:
			fmt.Fprintf(os.Stderr, "unknown video subcommand: %s\n", os.Args[2])
			os.Exit(1)
		}
	case "help", "-h", "--help":
		usage()
	default:
		fmt.Fprintf(os.Stderr, "unknown command: %s\n", os.Args[1])
		os.Exit(1)
	}
}

func usage() {
	fmt.Fprintln(os.Stderr, `starctl — star visual novel engine CLI

Usage:
  starctl video import <file> [--name NAME] [--codec CODEC] [--duration MS]
  starctl video palindrome <input-file> [--output FILE]
  starctl video list
  starctl help`)
}

func cmdVideoImport(args []string) {
	if len(args) < 1 {
		fmt.Fprintln(os.Stderr, "usage: starctl video import <file> [--name NAME] [--codec CODEC] [--duration MS]")
		os.Exit(1)
	}

	filePath := args[0]
	name := filePath
	codec := "h264"
	durationMS := 0

	// Simple flag parsing.
	for i := 1; i < len(args); i++ {
		switch args[i] {
		case "--name":
			i++
			name = args[i]
		case "--codec":
			i++
			codec = args[i]
		case "--duration":
			i++
			var err error
			durationMS, err = strconv.Atoi(args[i])
			if err != nil {
				fmt.Fprintf(os.Stderr, "invalid duration: %s\n", args[i])
				os.Exit(1)
			}
		}
	}

	data, err := os.ReadFile(filePath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "read file: %v\n", err)
		os.Exit(1)
	}

	ctx := context.Background()
	db := mustConnect(ctx)
	defer db.Close()

	v, err := db.ImportVideo(ctx, database.ImportVideoParams{
		Name:       name,
		Codec:      codec,
		MimeType:   "video/mp4",
		DurationMS: durationMS,
	}, bytes.NewReader(data))
	if err != nil {
		fmt.Fprintf(os.Stderr, "import video: %v\n", err)
		os.Exit(1)
	}

	fmt.Printf("imported: %s (%s, %d bytes)\n", v.VideoID, v.Name, v.SizeBytes)
}

func cmdVideoPalindrome(args []string) {
	if len(args) < 1 {
		fmt.Fprintln(os.Stderr, "usage: starctl video palindrome <input-file> [--output FILE]")
		os.Exit(1)
	}

	inputPath := args[0]
	outputPath := "palindrome_output.mp4"

	for i := 1; i < len(args); i++ {
		if args[i] == "--output" {
			i++
			outputPath = args[i]
		}
	}

	ctx := context.Background()
	if err := video.GeneratePalindrome(ctx, inputPath, outputPath); err != nil {
		fmt.Fprintf(os.Stderr, "generate palindrome: %v\n", err)
		os.Exit(1)
	}

	fmt.Printf("palindrome written to: %s\n", outputPath)
}

func cmdVideoList() {
	ctx := context.Background()
	db := mustConnect(ctx)
	defer db.Close()

	videos, err := db.ListVideos(ctx)
	if err != nil {
		fmt.Fprintf(os.Stderr, "list videos: %v\n", err)
		os.Exit(1)
	}

	if len(videos) == 0 {
		fmt.Println("no videos")
		return
	}

	for _, v := range videos {
		fmt.Printf("%s  %-30s  %s  %s  %d bytes  %s\n",
			v.VideoID, v.Name, v.Codec, v.LoopType, v.SizeBytes,
			v.CreatedAt.Format("2006-01-02 15:04"))
	}
}

func mustConnect(ctx context.Context) *database.DB {
	connStr := os.Getenv("STAR_DATABASE_URL")
	if connStr == "" {
		connStr = defaultConnStr
	}
	db, err := database.New(ctx, connStr)
	if err != nil {
		fmt.Fprintf(os.Stderr, "connect to database: %v\n", err)
		os.Exit(1)
	}
	return db
}
