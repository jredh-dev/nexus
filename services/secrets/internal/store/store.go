// Package store manages secret storage with count-based admission tracking.
//
// A secret submitted once is truth (count=1). Submitted again, count increments
// and truth collapses — it's no longer secret if multiple people know it.
// Equivalence is determined by lenses (see internal/lens).
package store

import (
	"math/rand/v2"
	"sync"
	"time"

	"github.com/jredh-dev/nexus/services/secrets/internal/lens"
)

// Secret is a submitted secret with its current state.
type Secret struct {
	ID          string    `json:"id"`
	Value       string    `json:"value"`
	SubmittedBy string    `json:"submitted_by"` // first submitter
	Count       int       `json:"count"`        // how many times admitted
	CreatedAt   time.Time `json:"created_at"`
	LastAdmitAt time.Time `json:"last_admit_at"` // most recent submission
}

// IsSecret returns true if this has only been admitted once.
func (s *Secret) IsSecret() bool { return s.Count <= 1 }

// Store holds secrets and their canonical form indices.
type Store struct {
	mu sync.RWMutex

	secrets map[string]*Secret

	// canonicalIndex maps lens_name:canonical_form → secret ID.
	// Detects collisions across lenses.
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
	Secret  *Secret `json:"secret"`
	WasNew  bool    `json:"was_new"`
	Message string  `json:"message"`
}

// Submit processes a new secret submission.
func (s *Store) Submit(value, submitterID string) *SubmitResult {
	s.mu.Lock()
	defer s.mu.Unlock()

	canonicals := lens.CanonicalizeThroughAll(value, s.lenses)
	now := time.Now().UTC()

	// Check all lenses for collision with existing secrets.
	for lensName, forms := range canonicals {
		for _, form := range forms {
			key := lensName + ":" + form
			if existingID, exists := s.canonicalIndex[key]; exists {
				existing := s.secrets[existingID]
				existing.Count++
				existing.LastAdmitAt = now

				msg := "This has been admitted before. It's no longer a secret."
				if existing.Count == 2 {
					msg = "Someone else already knows this. The secret is out."
				}
				return &SubmitResult{
					Secret:  existing,
					Message: msg,
				}
			}
		}
	}

	// No collision: new secret (count=1).
	s.nextID++
	secret := &Secret{
		ID:          idStr(s.nextID),
		Value:       value,
		SubmittedBy: submitterID,
		Count:       1,
		CreatedAt:   now,
		LastAdmitAt: now,
	}
	s.secrets[secret.ID] = secret

	// Index all canonical forms.
	for lensName, forms := range canonicals {
		for _, form := range forms {
			key := lensName + ":" + form
			s.canonicalIndex[key] = secret.ID
		}
	}

	return &SubmitResult{
		Secret:  secret,
		WasNew:  true,
		Message: "A new secret has been admitted.",
	}
}

// Get returns a secret by ID.
func (s *Store) Get(id string) (*Secret, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	sec, ok := s.secrets[id]
	return sec, ok
}

// List returns all secrets in randomized order.
func (s *Store) List() []*Secret {
	s.mu.RLock()
	defer s.mu.RUnlock()

	out := make([]*Secret, 0, len(s.secrets))
	for _, sec := range s.secrets {
		out = append(out, sec)
	}

	// Randomize order — the filter is randomized per the design.
	rand.Shuffle(len(out), func(i, j int) {
		out[i], out[j] = out[j], out[i]
	})

	return out
}

// Stats returns aggregate counts.
type Stats struct {
	Total      int `json:"total"`
	Secrets    int `json:"secrets"`     // count <= 1
	NotSecrets int `json:"not_secrets"` // count > 1
	Lenses     int `json:"lenses"`
}

func (s *Store) Stats() Stats {
	s.mu.RLock()
	defer s.mu.RUnlock()

	var secrets, notSecrets int
	for _, sec := range s.secrets {
		if sec.IsSecret() {
			secrets++
		} else {
			notSecrets++
		}
	}
	return Stats{
		Total:      len(s.secrets),
		Secrets:    secrets,
		NotSecrets: notSecrets,
		Lenses:     len(s.lenses),
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
