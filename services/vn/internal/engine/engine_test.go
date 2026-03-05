package engine

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

// --- Story loading and validation ---

func TestLoadStory_Valid(t *testing.T) {
	dir := t.TempDir()
	writeYAML(t, dir, "story.yaml", `
version: 1
title: "Test Story"
start_node: "ch1.intro"
chapters:
  ch1:
    title: "Chapter 1"
    sort_order: 1
    token_reward: 10
    start_node: intro
    nodes:
      intro:
        video_ref: "video_001"
        text: "Welcome"
        next_node: middle
      middle:
        video_ref: "video_002"
        choices:
          - label: "Go left"
            target: ending_a
          - label: "Go right"
            target: ending_b
      ending_a:
        video_ref: "video_003"
        text: "You went left"
        is_end: true
      ending_b:
        video_ref: "video_004"
        text: "You went right"
        is_end: true
`)

	story, err := LoadStory(filepath.Join(dir, "story.yaml"))
	if err != nil {
		t.Fatalf("LoadStory: %v", err)
	}

	if story.Title != "Test Story" {
		t.Errorf("title = %q, want %q", story.Title, "Test Story")
	}
	if story.Version != 1 {
		t.Errorf("version = %d, want 1", story.Version)
	}
	if len(story.Chapters) != 1 {
		t.Errorf("chapters = %d, want 1", len(story.Chapters))
	}

	ch := story.Chapters["ch1"]
	if len(ch.Nodes) != 4 {
		t.Errorf("ch1 nodes = %d, want 4", len(ch.Nodes))
	}

	// Check fully qualified IDs.
	intro := ch.Nodes["intro"]
	if intro.ID != "ch1.intro" {
		t.Errorf("intro.ID = %q, want %q", intro.ID, "ch1.intro")
	}
}

func TestLoadStory_InvalidRef(t *testing.T) {
	dir := t.TempDir()
	writeYAML(t, dir, "bad.yaml", `
version: 1
title: "Bad Story"
start_node: "ch1.intro"
chapters:
  ch1:
    title: "Chapter 1"
    start_node: intro
    nodes:
      intro:
        video_ref: "v1"
        next_node: nonexistent
`)

	_, err := LoadStory(filepath.Join(dir, "bad.yaml"))
	if err == nil {
		t.Fatal("expected validation error for bad next_node ref")
	}
}

func TestLoadStory_NoExits(t *testing.T) {
	dir := t.TempDir()
	writeYAML(t, dir, "no_exit.yaml", `
version: 1
title: "No Exit"
start_node: "ch1.intro"
chapters:
  ch1:
    title: "Chapter 1"
    start_node: intro
    nodes:
      intro:
        video_ref: "v1"
`)

	_, err := LoadStory(filepath.Join(dir, "no_exit.yaml"))
	if err == nil {
		t.Fatal("expected validation error for node with no exits")
	}
}

func TestLoadStory_MultipleExits(t *testing.T) {
	dir := t.TempDir()
	writeYAML(t, dir, "multi.yaml", `
version: 1
title: "Multi Exit"
start_node: "ch1.intro"
chapters:
  ch1:
    title: "Chapter 1"
    start_node: intro
    nodes:
      intro:
        video_ref: "v1"
        next_node: intro
        is_end: true
`)

	_, err := LoadStory(filepath.Join(dir, "multi.yaml"))
	if err == nil {
		t.Fatal("expected validation error for node with multiple exits")
	}
}

func TestLoadStoryDir_Merge(t *testing.T) {
	dir := t.TempDir()

	// First file: base story + chapter 1.
	writeYAML(t, dir, "01_base.yaml", `
version: 1
title: "Merged Story"
start_node: "ch1.start"
chapters:
  ch1:
    title: "Chapter 1"
    sort_order: 1
    start_node: start
    nodes:
      start:
        video_ref: "v1"
        next_node: ch2.begin
`)

	// Second file: chapter 2.
	writeYAML(t, dir, "02_ch2.yaml", `
version: 1
title: "ignored"
start_node: "ignored"
chapters:
  ch2:
    title: "Chapter 2"
    sort_order: 2
    token_reward: 5
    start_node: begin
    nodes:
      begin:
        video_ref: "v2"
        is_end: true
`)

	story, err := LoadStoryDir(dir)
	if err != nil {
		t.Fatalf("LoadStoryDir: %v", err)
	}

	if story.Title != "Merged Story" {
		t.Errorf("title = %q, want %q (first file's title)", story.Title, "Merged Story")
	}
	if len(story.Chapters) != 2 {
		t.Errorf("chapters = %d, want 2", len(story.Chapters))
	}

	order := story.ChapterOrder()
	if len(order) != 2 || order[0] != "ch1" || order[1] != "ch2" {
		t.Errorf("chapter order = %v, want [ch1, ch2]", order)
	}
}

func TestLoadStoryDir_DuplicateChapter(t *testing.T) {
	dir := t.TempDir()

	writeYAML(t, dir, "01.yaml", `
version: 1
title: "Dup"
start_node: "ch1.x"
chapters:
  ch1:
    title: "Chapter 1"
    start_node: x
    nodes:
      x:
        video_ref: "v"
        is_end: true
`)
	writeYAML(t, dir, "02.yaml", `
version: 1
title: "Dup2"
start_node: "ch1.x"
chapters:
  ch1:
    title: "Chapter 1 Again"
    start_node: x
    nodes:
      x:
        video_ref: "v"
        is_end: true
`)

	_, err := LoadStoryDir(dir)
	if err == nil {
		t.Fatal("expected error for duplicate chapter ID")
	}
}

// --- Navigator (state machine) ---

func buildTestStory(t *testing.T) *Story {
	t.Helper()
	dir := t.TempDir()
	writeYAML(t, dir, "story.yaml", `
version: 1
title: "Navigator Test"
start_node: "ch1.intro"
chapters:
  ch1:
    title: "Chapter 1"
    sort_order: 1
    token_reward: 10
    start_node: intro
    nodes:
      intro:
        video_ref: "v1"
        next_node: fork
      fork:
        video_ref: "v2"
        choices:
          - label: "Path A"
            target: ch2.scene_a
          - label: "Path B"
            target: ch2.scene_b
  ch2:
    title: "Chapter 2"
    sort_order: 2
    token_reward: 20
    start_node: scene_a
    nodes:
      scene_a:
        video_ref: "v3"
        is_end: true
      scene_b:
        video_ref: "v4"
        is_end: true
`)
	s, err := LoadStory(filepath.Join(dir, "story.yaml"))
	if err != nil {
		t.Fatalf("buildTestStory: %v", err)
	}
	return s
}

func TestNavigator_StartAndCurrent(t *testing.T) {
	story := buildTestStory(t)
	nav := NewNavigator(story)

	state, node, err := nav.Start("reader1")
	if err != nil {
		t.Fatalf("Start: %v", err)
	}

	if state.CurrentNode != "ch1.intro" {
		t.Errorf("current = %q, want %q", state.CurrentNode, "ch1.intro")
	}
	if node.VideoRef != "v1" {
		t.Errorf("video_ref = %q, want %q", node.VideoRef, "v1")
	}
	if len(state.Visited) != 1 || state.Visited[0] != "ch1.intro" {
		t.Errorf("visited = %v, want [ch1.intro]", state.Visited)
	}

	// CurrentNode should return same thing.
	state2, node2, err := nav.CurrentNode("reader1")
	if err != nil {
		t.Fatalf("CurrentNode: %v", err)
	}
	if state2.CurrentNode != state.CurrentNode {
		t.Errorf("CurrentNode mismatch")
	}
	if node2.ID != node.ID {
		t.Errorf("node mismatch")
	}
}

func TestNavigator_StartIdempotent(t *testing.T) {
	story := buildTestStory(t)
	nav := NewNavigator(story)

	_, _, _ = nav.Start("r1")
	_, _, _, _ = nav.Advance("r1", -1) // move to fork

	state, _, _ := nav.Start("r1") // should NOT reset
	if state.CurrentNode != "ch1.fork" {
		t.Errorf("Start should not reset; current = %q, want ch1.fork", state.CurrentNode)
	}
}

func TestNavigator_LinearAdvance(t *testing.T) {
	story := buildTestStory(t)
	nav := NewNavigator(story)
	_, _, _ = nav.Start("r1")

	state, node, completed, err := nav.Advance("r1", -1)
	if err != nil {
		t.Fatalf("Advance linear: %v", err)
	}

	if state.CurrentNode != "ch1.fork" {
		t.Errorf("current = %q, want ch1.fork", state.CurrentNode)
	}
	if node.VideoRef != "v2" {
		t.Errorf("video = %q, want v2", node.VideoRef)
	}
	if completed != "" {
		t.Errorf("completed = %q, want empty (same chapter)", completed)
	}
	if len(state.Visited) != 2 {
		t.Errorf("visited len = %d, want 2", len(state.Visited))
	}
}

func TestNavigator_ChoiceAdvance_CrossChapter(t *testing.T) {
	story := buildTestStory(t)
	nav := NewNavigator(story)
	_, _, _ = nav.Start("r1")
	_, _, _, _ = nav.Advance("r1", -1) // intro → fork

	// Choose "Path A" (index 0) → ch2.scene_a
	state, node, completed, err := nav.Advance("r1", 0)
	if err != nil {
		t.Fatalf("Advance choice: %v", err)
	}

	if state.CurrentNode != "ch2.scene_a" {
		t.Errorf("current = %q, want ch2.scene_a", state.CurrentNode)
	}
	if node.VideoRef != "v3" {
		t.Errorf("video = %q, want v3", node.VideoRef)
	}
	if completed != "ch1" {
		t.Errorf("completed = %q, want ch1", completed)
	}
	if len(state.Completed) != 1 || state.Completed[0] != "ch1" {
		t.Errorf("completed list = %v, want [ch1]", state.Completed)
	}
}

func TestNavigator_AdvanceFromEnd(t *testing.T) {
	story := buildTestStory(t)
	nav := NewNavigator(story)
	_, _, _ = nav.Start("r1")
	_, _, _, _ = nav.Advance("r1", -1) // intro → fork
	_, _, _, _ = nav.Advance("r1", 1)  // fork → ch2.scene_b

	_, _, _, err := nav.Advance("r1", -1)
	if err == nil {
		t.Fatal("expected error advancing from end node")
	}
}

func TestNavigator_InvalidChoiceIndex(t *testing.T) {
	story := buildTestStory(t)
	nav := NewNavigator(story)
	_, _, _ = nav.Start("r1")
	_, _, _, _ = nav.Advance("r1", -1) // → fork

	_, _, _, err := nav.Advance("r1", 5)
	if err == nil {
		t.Fatal("expected error for out-of-range choice index")
	}
}

func TestNavigator_ChoiceOnLinearNode(t *testing.T) {
	story := buildTestStory(t)
	nav := NewNavigator(story)
	_, _, _ = nav.Start("r1")

	// intro is a linear node (next_node), passing choiceIdx should error.
	_, _, _, err := nav.Advance("r1", 0)
	if err == nil {
		t.Fatal("expected error passing choice index to linear node")
	}
}

func TestNavigator_Reset(t *testing.T) {
	story := buildTestStory(t)
	nav := NewNavigator(story)
	_, _, _ = nav.Start("r1")
	_, _, _, _ = nav.Advance("r1", -1)

	nav.Reset("r1")

	_, _, err := nav.CurrentNode("r1")
	if err == nil {
		t.Fatal("expected error after reset (reader not started)")
	}

	// Re-start should go back to intro.
	state, _, err := nav.Start("r1")
	if err != nil {
		t.Fatalf("Start after reset: %v", err)
	}
	if state.CurrentNode != "ch1.intro" {
		t.Errorf("current = %q, want ch1.intro after reset", state.CurrentNode)
	}
}

func TestNavigator_SetStory(t *testing.T) {
	story := buildTestStory(t)
	nav := NewNavigator(story)
	_, _, _ = nav.Start("r1")

	// Replace the story.
	newStory := buildTestStory(t)
	newStory.Title = "Updated"
	nav.SetStory(newStory)

	if nav.Story().Title != "Updated" {
		t.Errorf("story title = %q, want %q", nav.Story().Title, "Updated")
	}

	// Reader state is preserved — current node still valid in new story.
	state, _, err := nav.CurrentNode("r1")
	if err != nil {
		t.Fatalf("CurrentNode after SetStory: %v", err)
	}
	if state.CurrentNode != "ch1.intro" {
		t.Errorf("current = %q, want ch1.intro", state.CurrentNode)
	}
}

// --- HotLoader ---

func TestHotLoader_InitialLoad(t *testing.T) {
	dir := t.TempDir()
	writeYAML(t, dir, "story.yaml", `
version: 1
title: "Hot Test"
start_node: "ch1.x"
chapters:
  ch1:
    title: "C1"
    start_node: x
    nodes:
      x:
        video_ref: "v1"
        is_end: true
`)

	hl, err := NewHotLoader(dir, nil)
	if err != nil {
		t.Fatalf("NewHotLoader: %v", err)
	}
	defer hl.Close()

	s := hl.Story()
	if s.Title != "Hot Test" {
		t.Errorf("title = %q, want %q", s.Title, "Hot Test")
	}
}

func TestHotLoader_Reload(t *testing.T) {
	dir := t.TempDir()
	writeYAML(t, dir, "story.yaml", `
version: 1
title: "Before"
start_node: "ch1.x"
chapters:
  ch1:
    title: "C1"
    start_node: x
    nodes:
      x:
        video_ref: "v1"
        is_end: true
`)

	reloaded := make(chan *Story, 1)
	hl, err := NewHotLoader(dir, func(s *Story) {
		reloaded <- s
	})
	if err != nil {
		t.Fatalf("NewHotLoader: %v", err)
	}
	defer hl.Close()

	// Overwrite the file.
	writeYAML(t, dir, "story.yaml", `
version: 2
title: "After"
start_node: "ch1.x"
chapters:
  ch1:
    title: "C1"
    start_node: x
    nodes:
      x:
        video_ref: "v1"
        is_end: true
`)

	select {
	case s := <-reloaded:
		if s.Title != "After" {
			t.Errorf("reloaded title = %q, want %q", s.Title, "After")
		}
		if s.Version != 2 {
			t.Errorf("reloaded version = %d, want 2", s.Version)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("timed out waiting for reload callback")
	}

	// hl.Story() should also reflect the update.
	if hl.Story().Title != "After" {
		t.Errorf("hl.Story().Title = %q, want %q", hl.Story().Title, "After")
	}
}

func TestHotLoader_BadReloadKeepsOld(t *testing.T) {
	dir := t.TempDir()
	writeYAML(t, dir, "story.yaml", `
version: 1
title: "Good"
start_node: "ch1.x"
chapters:
  ch1:
    title: "C1"
    start_node: x
    nodes:
      x:
        video_ref: "v1"
        is_end: true
`)

	hl, err := NewHotLoader(dir, nil)
	if err != nil {
		t.Fatalf("NewHotLoader: %v", err)
	}
	defer hl.Close()

	// Write invalid YAML — should fail to parse, old story preserved.
	writeYAML(t, dir, "story.yaml", `
version: 0
title: ""
start_node: ""
`)

	// Give the watcher time to process.
	time.Sleep(500 * time.Millisecond)

	if hl.Story().Title != "Good" {
		t.Errorf("story should still be %q after bad reload, got %q", "Good", hl.Story().Title)
	}
}

// --- ResolveRef ---

func TestResolveRef_Valid(t *testing.T) {
	story := buildTestStory(t)

	ch, node, err := story.ResolveRef("ch1.intro")
	if err != nil {
		t.Fatalf("ResolveRef: %v", err)
	}
	if ch.Title != "Chapter 1" {
		t.Errorf("chapter = %q", ch.Title)
	}
	if node.VideoRef != "v1" {
		t.Errorf("video = %q", node.VideoRef)
	}
}

func TestResolveRef_BadFormat(t *testing.T) {
	story := buildTestStory(t)
	_, _, err := story.ResolveRef("noDot")
	if err == nil {
		t.Fatal("expected error for ref without dot")
	}
}

func TestResolveRef_MissingChapter(t *testing.T) {
	story := buildTestStory(t)
	_, _, err := story.ResolveRef("fake.node")
	if err == nil {
		t.Fatal("expected error for missing chapter")
	}
}

func TestResolveRef_MissingNode(t *testing.T) {
	story := buildTestStory(t)
	_, _, err := story.ResolveRef("ch1.fake")
	if err == nil {
		t.Fatal("expected error for missing node")
	}
}

// --- Helpers ---

func writeYAML(t *testing.T, dir, name, content string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(dir, name), []byte(content), 0644); err != nil {
		t.Fatalf("writeYAML %s: %v", name, err)
	}
}
