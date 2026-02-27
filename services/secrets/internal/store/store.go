// Package store manages secret storage and truth/lie state transitions.
package store

import (
	"sync"
	"time"

	"github.com/jredh-dev/nexus/services/secrets/internal/lens"
)

// State represents whether a secret is truth or lie.
type State string

const (
	Truth State = "truth"
	Lie   State = "lie"
)

// Secret is a submitted secret with its current state.
type Secret struct {
	ID          string     `json:"id"`
	Value       string     `json:"value"`
	SubmittedBy string     `json:"submitted_by"`
	State       State      `json:"state"`
	ExposedBy   string     `json:"exposed_by,omitempty"`
	ExposedVia  string     `json:"exposed_via,omitempty"` // which lens caused the collapse
	CreatedAt   time.Time  `json:"created_at"`
	ExposedAt   *time.Time `json:"exposed_at,omitempty"`
}

// Exposure records how a submission exposed an existing secret.
type Exposure struct {
	SecretID   string
	LensName   string
	Canonical  string
	ExposerID  string
	ExposerVal string
}

// Store holds secrets and their canonical form indices.
type Store struct {
	mu sync.RWMutex

	// secrets by ID
	secrets map[string]*Secret

	// canonicalIndex maps lens_name:canonical_form → secret ID
	// This is how we detect collisions across lenses.
	canonicalIndex map[string]string

	lenses []lens.Lens
	nextID int
}

// New creates a new in-memory store with the default lens set.
func New() *Store {
	return &Store{
		secrets:        make(map[string]*Secret),
		canonicalIndex: make(map[string]string),
		lenses:         lens.All(),
	}
}

// SubmitResult describes what happened when a secret was submitted.
type SubmitResult struct {
	// The submitted secret (newly created, or the one that was exposed)
	Secret *Secret `json:"secret"`

	// If this submission exposed an existing secret
	Exposed *Exposure `json:"-"`

	// SelfBetrayal is true if the secret was a palindrome (betrayed itself)
	SelfBetrayal bool `json:"self_betrayal,omitempty"`

	// WasNew is true if this created a new truth
	WasNew bool `json:"was_new"`
}

// Submit processes a new secret submission.
func (s *Store) Submit(value, submitterID string) *SubmitResult {
	s.mu.Lock()
	defer s.mu.Unlock()

	canonicals := lens.CanonicalizeThroughAll(value, s.lenses)

	// Check for palindrome self-betrayal first
	if palForms, ok := canonicals["palindrome"]; ok {
		for _, form := range palForms {
			s.nextID++
			now := time.Now().UTC()
			secret := &Secret{
				ID:          idStr(s.nextID),
				Value:       value,
				SubmittedBy: submitterID,
				State:       Lie,
				ExposedBy:   submitterID,
				ExposedVia:  "palindrome",
				CreatedAt:   now,
				ExposedAt:   &now,
			}
			s.secrets[secret.ID] = secret

			return &SubmitResult{
				Secret:       secret,
				SelfBetrayal: true,
				Exposed: &Exposure{
					SecretID:   secret.ID,
					LensName:   "palindrome",
					Canonical:  form,
					ExposerID:  submitterID,
					ExposerVal: value,
				},
			}
		}
	}

	// Check all lenses for collision with existing secrets
	for lensName, forms := range canonicals {
		if lensName == "palindrome" {
			continue // already handled
		}
		for _, form := range forms {
			key := lensName + ":" + form
			if existingID, exists := s.canonicalIndex[key]; exists {
				existing := s.secrets[existingID]
				if existing.State == Truth {
					now := time.Now().UTC()
					existing.State = Lie
					existing.ExposedBy = submitterID
					existing.ExposedVia = lensName
					existing.ExposedAt = &now

					return &SubmitResult{
						Secret: existing,
						Exposed: &Exposure{
							SecretID:   existingID,
							LensName:   lensName,
							Canonical:  form,
							ExposerID:  submitterID,
							ExposerVal: value,
						},
					}
				}
			}
		}
	}

	// No collision: create new truth
	s.nextID++
	secret := &Secret{
		ID:          idStr(s.nextID),
		Value:       value,
		SubmittedBy: submitterID,
		State:       Truth,
		CreatedAt:   time.Now().UTC(),
	}
	s.secrets[secret.ID] = secret

	// Index all canonical forms
	for lensName, forms := range canonicals {
		if lensName == "palindrome" {
			continue
		}
		for _, form := range forms {
			key := lensName + ":" + form
			s.canonicalIndex[key] = secret.ID
		}
	}

	return &SubmitResult{
		Secret: secret,
		WasNew: true,
	}
}

// Get returns a secret by ID.
func (s *Store) Get(id string) (*Secret, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	sec, ok := s.secrets[id]
	return sec, ok
}

// List returns all secrets.
func (s *Store) List() []*Secret {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make([]*Secret, 0, len(s.secrets))
	for _, sec := range s.secrets {
		out = append(out, sec)
	}
	return out
}

// Stats returns aggregate counts.
func (s *Store) Stats() map[string]int {
	s.mu.RLock()
	defer s.mu.RUnlock()
	truths, lies := 0, 0
	for _, sec := range s.secrets {
		switch sec.State {
		case Truth:
			truths++
		case Lie:
			lies++
		}
	}
	return map[string]int{
		"total":  len(s.secrets),
		"truths": truths,
		"lies":   lies,
		"lenses": len(s.lenses),
	}
}

func idStr(n int) string {
	return "sec_" + time.Now().Format("20060102") + "_" + padInt(n)
}

func padInt(n int) string {
	s := ""
	if n < 10 {
		s = "000"
	} else if n < 100 {
		s = "00"
	} else if n < 1000 {
		s = "0"
	}
	return s + itoa(n)
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	var digits []byte
	for n > 0 {
		digits = append([]byte{byte('0' + n%10)}, digits...)
		n /= 10
	}
	return string(digits)
}
