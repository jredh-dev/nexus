package auth

import (
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jredh-dev/nexus/services/portal/config"
	"github.com/jredh-dev/nexus/services/portal/internal/database"
	"github.com/jredh-dev/nexus/services/portal/pkg/models"
	"golang.org/x/crypto/bcrypt"
)

const bcryptCost = 12

// Service handles authentication operations.
type Service struct {
	db  *database.DB
	cfg *config.Config
}

// New creates a new auth service.
func New(db *database.DB, cfg *config.Config) *Service {
	return &Service{db: db, cfg: cfg}
}

// HashPassword hashes a plaintext password with bcrypt.
func HashPassword(password string) (string, error) {
	hash, err := bcrypt.GenerateFromPassword([]byte(password), bcryptCost)
	if err != nil {
		return "", fmt.Errorf("hash password: %w", err)
	}
	return string(hash), nil
}

// CheckPassword compares a plaintext password against a bcrypt hash.
func CheckPassword(password, hash string) error {
	return bcrypt.CompareHashAndPassword([]byte(hash), []byte(password))
}

// Login verifies credentials and creates a new session.
// Returns the session ID (used as cookie value).
func (s *Service) Login(email, password, ipAddress, userAgent string) (string, error) {
	user, err := s.db.GetUserByEmail(email)
	if err != nil {
		return "", fmt.Errorf("lookup user: %w", err)
	}
	if user == nil {
		return "", ErrInvalidCredentials
	}

	if err := CheckPassword(password, user.PasswordHash); err != nil {
		return "", ErrInvalidCredentials
	}

	// Create session
	now := time.Now()
	session := &models.Session{
		ID:        uuid.New().String(),
		UserID:    user.ID,
		ExpiresAt: now.Add(time.Duration(s.cfg.Session.MaxAge) * time.Second),
		CreatedAt: now,
		IPAddress: ipAddress,
		UserAgent: userAgent,
	}

	if err := s.db.CreateSession(session); err != nil {
		return "", fmt.Errorf("create session: %w", err)
	}

	// Update last login
	if err := s.db.UpdateLastLogin(user.ID, now); err != nil {
		return "", fmt.Errorf("update last login: %w", err)
	}

	return session.ID, nil
}

// ValidateSession looks up a session by ID and returns the associated user.
// Returns (nil, nil) if the session does not exist or has expired.
func (s *Service) ValidateSession(sessionID string) (*models.User, *models.Session, error) {
	session, err := s.db.GetSession(sessionID)
	if err != nil {
		return nil, nil, fmt.Errorf("get session: %w", err)
	}
	if session == nil {
		return nil, nil, nil
	}

	user, err := s.db.GetUserByID(session.UserID)
	if err != nil {
		return nil, nil, fmt.Errorf("get user: %w", err)
	}
	if user == nil {
		// Orphaned session — clean it up.
		_ = s.db.DeleteSession(sessionID)
		return nil, nil, nil
	}

	return user, session, nil
}

// Logout deletes a session.
func (s *Service) Logout(sessionID string) error {
	return s.db.DeleteSession(sessionID)
}

// CreateUser registers a new user with a hashed password.
func (s *Service) CreateUser(email, password, name string) (*models.User, error) {
	hash, err := HashPassword(password)
	if err != nil {
		return nil, err
	}

	now := time.Now()
	user := &models.User{
		ID:           uuid.New().String(),
		Email:        email,
		Name:         name,
		PasswordHash: hash,
		CreatedAt:    now,
		UpdatedAt:    now,
		LastLoginAt:  now,
	}

	if err := s.db.CreateUser(user); err != nil {
		return nil, fmt.Errorf("create user: %w", err)
	}
	return user, nil
}

// GetSessionsByUserID returns all active sessions for a user.
func (s *Service) GetSessionsByUserID(userID string) ([]models.Session, error) {
	return s.db.GetSessionsByUserID(userID)
}

// CleanExpiredSessions removes all expired sessions from the database.
func (s *Service) CleanExpiredSessions() error {
	return s.db.DeleteExpiredSessions()
}
