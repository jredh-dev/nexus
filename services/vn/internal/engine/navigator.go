package engine

import (
	"fmt"
	"sync"
)

// ReaderState tracks a single reader's position in the story graph.
type ReaderState struct {
	CurrentNode string   `json:"current_node"` // fully qualified: "chapter.node"
	Visited     []string `json:"visited"`      // history of visited node IDs
	Completed   []string `json:"completed"`    // chapter IDs completed
}

// Navigator provides a state machine for traversing the story graph.
// It is safe for concurrent use — each reader has independent state
// protected by the mutex.
type Navigator struct {
	story   *Story
	mu      sync.RWMutex
	readers map[string]*ReaderState // keyed by reader ID (device hash or uuid)
}

// NewNavigator creates a navigator for the given story.
func NewNavigator(story *Story) *Navigator {
	return &Navigator{
		story:   story,
		readers: make(map[string]*ReaderState),
	}
}

// Start initializes a reader at the story's start node. If the reader already
// has state, it returns their current position without resetting.
func (n *Navigator) Start(readerID string) (*ReaderState, *Node, error) {
	n.mu.Lock()
	defer n.mu.Unlock()

	if state, ok := n.readers[readerID]; ok {
		_, node, err := n.story.ResolveRef(state.CurrentNode)
		if err != nil {
			return nil, nil, fmt.Errorf("resolve current node: %w", err)
		}
		return state, node, nil
	}

	_, node, err := n.story.ResolveRef(n.story.StartNode)
	if err != nil {
		return nil, nil, fmt.Errorf("resolve start node: %w", err)
	}

	state := &ReaderState{
		CurrentNode: n.story.StartNode,
		Visited:     []string{n.story.StartNode},
	}
	n.readers[readerID] = state
	return state, node, nil
}

// CurrentNode returns the reader's current node, or an error if the reader
// hasn't started.
func (n *Navigator) CurrentNode(readerID string) (*ReaderState, *Node, error) {
	n.mu.RLock()
	defer n.mu.RUnlock()

	state, ok := n.readers[readerID]
	if !ok {
		return nil, nil, fmt.Errorf("reader %q has not started", readerID)
	}

	_, node, err := n.story.ResolveRef(state.CurrentNode)
	if err != nil {
		return nil, nil, fmt.Errorf("resolve current node: %w", err)
	}
	return state, node, nil
}

// Advance moves the reader to the next node. For linear nodes (next_node),
// no choiceIdx is needed — pass -1. For choice nodes, pass the index of the
// selected choice.
//
// Returns:
//   - the updated state
//   - the new node
//   - the chapter ID if a chapter was just completed (empty string otherwise)
//   - error if the transition is invalid
func (n *Navigator) Advance(readerID string, choiceIdx int) (*ReaderState, *Node, string, error) {
	n.mu.Lock()
	defer n.mu.Unlock()

	state, ok := n.readers[readerID]
	if !ok {
		return nil, nil, "", fmt.Errorf("reader %q has not started", readerID)
	}

	currentChapter, currentNode, err := n.story.ResolveRef(state.CurrentNode)
	if err != nil {
		return nil, nil, "", fmt.Errorf("resolve current: %w", err)
	}

	if currentNode.IsEnd {
		return nil, nil, "", fmt.Errorf("cannot advance from end node %q", state.CurrentNode)
	}

	var targetRef string

	if currentNode.NextNode != "" {
		// Linear transition.
		if choiceIdx >= 0 {
			return nil, nil, "", fmt.Errorf("node %q is linear (next_node), choiceIdx must be -1", state.CurrentNode)
		}
		targetRef = n.story.qualifyRef(chapterFromRef(state.CurrentNode), currentNode.NextNode)
	} else if len(currentNode.Choices) > 0 {
		// Choice transition.
		if choiceIdx < 0 || choiceIdx >= len(currentNode.Choices) {
			return nil, nil, "", fmt.Errorf("invalid choice index %d for node %q (has %d choices)",
				choiceIdx, state.CurrentNode, len(currentNode.Choices))
		}
		targetRef = n.story.qualifyRef(chapterFromRef(state.CurrentNode), currentNode.Choices[choiceIdx].TargetRef)
	} else {
		return nil, nil, "", fmt.Errorf("node %q has no exits", state.CurrentNode)
	}

	_, targetNode, err := n.story.ResolveRef(targetRef)
	if err != nil {
		return nil, nil, "", fmt.Errorf("resolve target %q: %w", targetRef, err)
	}

	// Check if we're crossing chapter boundaries.
	oldChapter := chapterFromRef(state.CurrentNode)
	newChapter := chapterFromRef(targetRef)
	completedChapter := ""
	if oldChapter != newChapter {
		// Mark the old chapter as completed (if not already).
		if !contains(state.Completed, oldChapter) {
			state.Completed = append(state.Completed, oldChapter)
			completedChapter = oldChapter
		}
	}

	// Check if the current node is an end node AND is the last node visited
	// in its chapter — also marks completion.
	_ = currentChapter // suppress unused; used for future chapter-end detection

	state.CurrentNode = targetRef
	state.Visited = append(state.Visited, targetRef)

	return state, targetNode, completedChapter, nil
}

// Reset clears a reader's state, allowing them to restart from the beginning.
func (n *Navigator) Reset(readerID string) {
	n.mu.Lock()
	defer n.mu.Unlock()
	delete(n.readers, readerID)
}

// SetStory atomically replaces the underlying story (used by hot-reload).
// Existing reader states are preserved — if their current node no longer
// exists in the new story, they'll get an error on next Advance/CurrentNode
// and should be reset.
func (n *Navigator) SetStory(story *Story) {
	n.mu.Lock()
	defer n.mu.Unlock()
	n.story = story
}

// Story returns the current story (for read-only access).
func (n *Navigator) Story() *Story {
	n.mu.RLock()
	defer n.mu.RUnlock()
	return n.story
}

// chapterFromRef extracts the chapter ID from a fully qualified ref.
func chapterFromRef(ref string) string {
	for i := 0; i < len(ref); i++ {
		if ref[i] == '.' {
			return ref[:i]
		}
	}
	return ref
}

// contains checks if a string slice contains the given value.
func contains(ss []string, val string) bool {
	for _, s := range ss {
		if s == val {
			return true
		}
	}
	return false
}
