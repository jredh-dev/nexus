// Package engine loads YAML story definitions and provides a chapter-based
// state machine for navigating branching narratives.
//
// A story is a directed graph of nodes (scenes). Each node references a video
// and subtitles, and optionally presents choices that lead to other nodes.
// Nodes are grouped into chapters. The engine resolves transitions within and
// across chapters, tracks reader position, and supports hot-reloading of
// story definitions without restarting the server.
package engine

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"gopkg.in/yaml.v3"
)

// Story is the top-level container parsed from YAML. A single story
// file defines the entire narrative graph.
type Story struct {
	Version     int                `yaml:"version" json:"version"`
	Title       string             `yaml:"title" json:"title"`
	Description string             `yaml:"description,omitempty" json:"description,omitempty"`
	StartNode   string             `yaml:"start_node" json:"start_node"`
	Chapters    map[string]Chapter `yaml:"chapters" json:"chapters"`
}

// Chapter groups related nodes into a narrative unit. Readers earn tokens
// upon completing a chapter.
type Chapter struct {
	Title       string           `yaml:"title" json:"title"`
	Description string           `yaml:"description,omitempty" json:"description,omitempty"`
	SortOrder   int              `yaml:"sort_order" json:"sort_order"`
	TokenReward int              `yaml:"token_reward" json:"token_reward"`
	Nodes       map[string]*Node `yaml:"nodes" json:"nodes"`
	StartNode   string           `yaml:"start_node" json:"start_node"`
}

// Node is a single scene in the story graph. It displays a video with
// subtitles and optionally presents choices to the reader.
type Node struct {
	// Fully qualified ID set at load time: "chapter_id.node_id"
	ID string `yaml:"-" json:"id"`

	VideoRef string     `yaml:"video_ref" json:"video_ref"`
	Text     string     `yaml:"text,omitempty" json:"text,omitempty"`
	Choices  []Choice   `yaml:"choices,omitempty" json:"choices,omitempty"`
	NextNode string     `yaml:"next_node,omitempty" json:"next_node,omitempty"`
	Subtitle []Subtitle `yaml:"subtitles,omitempty" json:"subtitles,omitempty"`

	// IsEnd marks this as a terminal node (no next_node, no choices).
	IsEnd bool `yaml:"is_end,omitempty" json:"is_end,omitempty"`
}

// Choice represents a branching option at a choice point. Readers vote
// using tokens to influence which path the story takes.
type Choice struct {
	Label     string `yaml:"label" json:"label"`
	TargetRef string `yaml:"target" json:"target"`
	TokenCost int    `yaml:"token_cost,omitempty" json:"token_cost,omitempty"`
}

// Subtitle defines inline subtitle data in the YAML. This is separate from
// the database Subtitle type — these get loaded into the DB or served directly
// depending on the mode.
type Subtitle struct {
	Text              string    `yaml:"text" json:"text"`
	InitializeVisible bool      `yaml:"initialize_visible" json:"initialize_visible"`
	EndVisible        bool      `yaml:"end_visible" json:"end_visible"`
	TimestampsVisible []float64 `yaml:"timestamps_visible" json:"timestamps_visible"`
	SortOrder         int       `yaml:"sort_order" json:"sort_order"`
}

// LoadStory reads a single YAML file and returns a validated Story.
func LoadStory(path string) (*Story, error) {
	s, err := parseStory(path)
	if err != nil {
		return nil, err
	}
	if err := s.Validate(); err != nil {
		return nil, fmt.Errorf("validate story %s: %w", path, err)
	}
	return s, nil
}

// parseStory reads and parses a YAML file without validation. Used by
// LoadStoryDir to merge multiple files before validating the combined result.
func parseStory(path string) (*Story, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read story file %s: %w", path, err)
	}

	var s Story
	if err := yaml.Unmarshal(data, &s); err != nil {
		return nil, fmt.Errorf("parse story file %s: %w", path, err)
	}

	s.populateNodeIDs()
	return &s, nil
}

// LoadStoryDir loads all .yaml/.yml files in a directory and merges them
// into a single Story. Files are processed in lexicographic order. The first
// file's top-level metadata (title, version, start_node) is used; subsequent
// files contribute only chapters.
func LoadStoryDir(dir string) (*Story, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, fmt.Errorf("read story dir %s: %w", dir, err)
	}

	var files []string
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		ext := strings.ToLower(filepath.Ext(e.Name()))
		if ext == ".yaml" || ext == ".yml" {
			files = append(files, filepath.Join(dir, e.Name()))
		}
	}
	sort.Strings(files)

	if len(files) == 0 {
		return nil, fmt.Errorf("no YAML files in %s", dir)
	}

	merged, err := parseStory(files[0])
	if err != nil {
		return nil, err
	}

	for _, f := range files[1:] {
		s, err := parseStory(f)
		if err != nil {
			return nil, err
		}
		for id, ch := range s.Chapters {
			if _, exists := merged.Chapters[id]; exists {
				return nil, fmt.Errorf("duplicate chapter %q in %s", id, f)
			}
			merged.Chapters[id] = ch
		}
	}

	merged.populateNodeIDs()
	if err := merged.Validate(); err != nil {
		return nil, fmt.Errorf("validate merged story: %w", err)
	}
	return merged, nil
}

// populateNodeIDs sets the fully qualified ID on each node.
func (s *Story) populateNodeIDs() {
	for chID, ch := range s.Chapters {
		for nodeID, node := range ch.Nodes {
			node.ID = chID + "." + nodeID
		}
	}
}

// Validate checks the story graph for structural errors:
//   - start_node must exist
//   - every chapter.start_node must exist
//   - all choice targets and next_node refs must resolve
//   - nodes must have exactly one of: choices, next_node, or is_end
func (s *Story) Validate() error {
	if s.Title == "" {
		return fmt.Errorf("story title is required")
	}
	if s.Version < 1 {
		return fmt.Errorf("story version must be >= 1, got %d", s.Version)
	}
	if len(s.Chapters) == 0 {
		return fmt.Errorf("story must have at least one chapter")
	}
	if s.StartNode == "" {
		return fmt.Errorf("story start_node is required")
	}

	// start_node format is "chapter.node"
	if _, _, err := s.ResolveRef(s.StartNode); err != nil {
		return fmt.Errorf("story start_node %q: %w", s.StartNode, err)
	}

	for chID, ch := range s.Chapters {
		if ch.Title == "" {
			return fmt.Errorf("chapter %q: title is required", chID)
		}
		if len(ch.Nodes) == 0 {
			return fmt.Errorf("chapter %q: must have at least one node", chID)
		}
		if ch.StartNode == "" {
			return fmt.Errorf("chapter %q: start_node is required", chID)
		}
		if _, ok := ch.Nodes[ch.StartNode]; !ok {
			return fmt.Errorf("chapter %q: start_node %q not found in nodes", chID, ch.StartNode)
		}

		for nodeID, node := range ch.Nodes {
			fqID := chID + "." + nodeID
			exits := 0
			if len(node.Choices) > 0 {
				exits++
			}
			if node.NextNode != "" {
				exits++
			}
			if node.IsEnd {
				exits++
			}
			if exits == 0 {
				return fmt.Errorf("node %q: must have choices, next_node, or is_end", fqID)
			}
			if exits > 1 {
				return fmt.Errorf("node %q: can only have one of choices, next_node, or is_end", fqID)
			}

			// Validate next_node reference.
			if node.NextNode != "" {
				ref := s.qualifyRef(chID, node.NextNode)
				if _, _, err := s.ResolveRef(ref); err != nil {
					return fmt.Errorf("node %q next_node %q: %w", fqID, node.NextNode, err)
				}
			}

			// Validate choice targets.
			for i, c := range node.Choices {
				if c.Label == "" {
					return fmt.Errorf("node %q choice[%d]: label is required", fqID, i)
				}
				ref := s.qualifyRef(chID, c.TargetRef)
				if _, _, err := s.ResolveRef(ref); err != nil {
					return fmt.Errorf("node %q choice[%d] target %q: %w", fqID, i, c.TargetRef, err)
				}
			}
		}
	}
	return nil
}

// ResolveRef takes a fully qualified ref ("chapter.node") and returns the
// chapter and node. Returns an error if either is not found.
func (s *Story) ResolveRef(ref string) (*Chapter, *Node, error) {
	parts := strings.SplitN(ref, ".", 2)
	if len(parts) != 2 {
		return nil, nil, fmt.Errorf("invalid ref %q: must be chapter.node", ref)
	}
	ch, ok := s.Chapters[parts[0]]
	if !ok {
		return nil, nil, fmt.Errorf("chapter %q not found", parts[0])
	}
	node, ok := ch.Nodes[parts[1]]
	if !ok {
		return nil, nil, fmt.Errorf("node %q not found in chapter %q", parts[1], parts[0])
	}
	return &ch, node, nil
}

// qualifyRef ensures a ref is fully qualified. If it contains a dot, it's
// returned as-is. Otherwise, it's prefixed with the current chapter ID.
func (s *Story) qualifyRef(currentChapter, ref string) string {
	if strings.Contains(ref, ".") {
		return ref
	}
	return currentChapter + "." + ref
}

// ChapterOrder returns chapter IDs sorted by SortOrder.
func (s *Story) ChapterOrder() []string {
	type kv struct {
		id    string
		order int
	}
	var pairs []kv
	for id, ch := range s.Chapters {
		pairs = append(pairs, kv{id, ch.SortOrder})
	}
	sort.Slice(pairs, func(i, j int) bool {
		if pairs[i].order != pairs[j].order {
			return pairs[i].order < pairs[j].order
		}
		return pairs[i].id < pairs[j].id
	})
	ids := make([]string, len(pairs))
	for i, p := range pairs {
		ids[i] = p.id
	}
	return ids
}

// NodesByChapter returns the node IDs in a chapter, sorted alphabetically.
// Useful for deterministic traversal in tests.
func (s *Story) NodesByChapter(chapterID string) []string {
	ch, ok := s.Chapters[chapterID]
	if !ok {
		return nil
	}
	var ids []string
	for id := range ch.Nodes {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	return ids
}
