package auth

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"log"
	"time"

	"github.com/google/uuid"
	"github.com/jredh-dev/nexus/internal/pgbus"
	"github.com/jredh-dev/nexus/services/portal/config"
	"github.com/jredh-dev/nexus/services/portal/internal/database"
	"github.com/jredh-dev/nexus/services/portal/internal/mailer"
	"github.com/jredh-dev/nexus/services/portal/pkg/identity"
	"github.com/jredh-dev/nexus/services/portal/pkg/models"
	"golang.org/x/crypto/bcrypt"
)

// bcryptCost is the work factor for bcrypt password hashing.
const bcryptCost = 12

// portalEventsChannel is the Postgres LISTEN/NOTIFY channel for portal events.
const portalEventsChannel = "portal.events"

// Service handles authentication operations.
type Service struct {
	db     *database.DB
	cfg    *config.Config
	mailer *mailer.Mailer
}

// New creates a new auth service.
func New(db *database.DB, cfg *config.Config) *Service {
	m := mailer.New(cfg.SMTP.Host, cfg.SMTP.Port, cfg.SMTP.From)
	return &Service{db: db, cfg: cfg, mailer: m}
}

// publish fires a pgbus event on portal.events, logging on error (non-fatal).
func (s *Service) publish(eventName string, data map[string]any) {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	if err := pgbus.Publish(ctx, s.db.Pool(), portalEventsChannel, eventName, "portal", data); err != nil {
		log.Printf("portal: publish %s: %v", eventName, err)
	}
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

// Signup registers a new user after checking for identity duplicates.
// It normalizes and hashes the email and phone number, then checks
// that no existing user shares the same username, email hash, or phone hash.
func (s *Service) Signup(username, email, phone, password, name string) (*models.User, error) {
	// Check username uniqueness.
	existing, err := s.db.GetUserByUsername(username)
	if err != nil {
		return nil, fmt.Errorf("check username: %w", err)
	}
	if existing != nil {
		return nil, ErrUsernameTaken
	}

	// Compute identity hashes.
	eHash := identity.EmailHash(email)
	pHash := identity.PhoneHash(phone)

	// Check email dedup.
	existing, err = s.db.GetUserByEmailHash(eHash)
	if err != nil {
		return nil, fmt.Errorf("check email hash: %w", err)
	}
	if existing != nil {
		return nil, ErrEmailTaken
	}

	// Check phone dedup.
	existing, err = s.db.GetUserByPhoneHash(pHash)
	if err != nil {
		return nil, fmt.Errorf("check phone hash: %w", err)
	}
	if existing != nil {
		return nil, ErrPhoneTaken
	}

	// Hash password.
	pwHash, err := HashPassword(password)
	if err != nil {
		return nil, err
	}

	now := time.Now()
	user := &models.User{
		ID:           uuid.New().String(),
		Username:     username,
		Email:        email,
		PhoneNumber:  phone,
		Name:         name,
		Role:         models.RoleUser,
		PasswordHash: pwHash,
		EmailHash:    eHash,
		PhoneHash:    pHash,
		CreatedAt:    now,
		UpdatedAt:    now,
		LastLoginAt:  now,
	}

	if err := s.db.CreateUser(user); err != nil {
		return nil, fmt.Errorf("create user: %w", err)
	}

	// Publish user.created event — non-fatal if it fails.
	s.publish("user.created", map[string]any{"user_id": user.ID, "email": user.Email})

	return user, nil
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

	// Publish user.login event — non-fatal if it fails.
	s.publish("user.login", map[string]any{"user_id": user.ID, "email": user.Email})

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
// This is the legacy method used by dev seeding. For user-facing signup,
// use Signup() which includes identity dedup checks.
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
		Role:         models.RoleUser,
		PasswordHash: hash,
		EmailHash:    identity.EmailHash(email),
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

// UpdateUserRole changes a user's role.
func (s *Service) UpdateUserRole(userID, role string) error {
	if role != models.RoleUser && role != models.RoleAdmin {
		return fmt.Errorf("invalid role: %s", role)
	}
	return s.db.UpdateUserRole(userID, role)
}

// --- Magic link operations ---

const (
	magicTokenBytes  = 32               // 256-bit token
	magicTokenExpiry = 15 * time.Minute // tokens expire after 15 minutes
)

// CreateMagicToken generates a one-time login token for the given user email.
// Returns the raw token string (hex-encoded).
func (s *Service) CreateMagicToken(email string) (string, error) {
	user, err := s.db.GetUserByEmail(email)
	if err != nil {
		return "", fmt.Errorf("lookup user: %w", err)
	}
	if user == nil {
		return "", ErrUserNotFound
	}

	// Generate a cryptographically secure random token.
	tokenBytes := make([]byte, magicTokenBytes)
	if _, err := rand.Read(tokenBytes); err != nil {
		return "", fmt.Errorf("generate token: %w", err)
	}
	token := hex.EncodeToString(tokenBytes)

	now := time.Now()
	mt := &models.MagicToken{
		ID:        token,
		UserID:    user.ID,
		ExpiresAt: now.Add(magicTokenExpiry),
		CreatedAt: now,
	}

	if err := s.db.CreateMagicToken(mt); err != nil {
		return "", fmt.Errorf("store magic token: %w", err)
	}

	return token, nil
}

// ValidateMagicToken checks and consumes a magic token, returning the user
// and creating a new session. Returns the session ID (cookie value).
func (s *Service) ValidateMagicToken(token, ipAddress, userAgent string) (string, error) {
	mt, err := s.db.GetMagicToken(token)
	if err != nil {
		return "", fmt.Errorf("get magic token: %w", err)
	}
	if mt == nil {
		return "", ErrInvalidMagicToken
	}

	// Consume the token so it can't be reused.
	if err := s.db.ConsumeMagicToken(token); err != nil {
		return "", fmt.Errorf("consume magic token: %w", err)
	}

	// Create a session for the user.
	now := time.Now()
	session := &models.Session{
		ID:        uuid.New().String(),
		UserID:    mt.UserID,
		ExpiresAt: now.Add(time.Duration(s.cfg.Session.MaxAge) * time.Second),
		CreatedAt: now,
		IPAddress: ipAddress,
		UserAgent: userAgent,
	}

	if err := s.db.CreateSession(session); err != nil {
		return "", fmt.Errorf("create session: %w", err)
	}

	if err := s.db.UpdateLastLogin(mt.UserID, now); err != nil {
		return "", fmt.Errorf("update last login: %w", err)
	}

	return session.ID, nil
}

// --- Email change operations ---

const emailChangeTokenExpiry = 15 * time.Minute

// InitiateEmailChange sends a verification email to newEmail containing a
// one-time-use token. The email change is not applied until ConfirmEmailChange
// is called with the token.
//
// baseURL should be the scheme+host used to build the confirmation link,
// e.g. "https://portal.jredh.com" or "http://localhost:8090".
func (s *Service) InitiateEmailChange(userID, newEmail, baseURL string) error {
	// Verify the user exists.
	user, err := s.db.GetUserByID(userID)
	if err != nil {
		return fmt.Errorf("lookup user: %w", err)
	}
	if user == nil {
		return ErrUserNotFound
	}

	// Reject if new email is already taken by another account.
	newHash := identity.EmailHash(newEmail)
	existing, err := s.db.GetUserByEmailHash(newHash)
	if err != nil {
		return fmt.Errorf("check email hash: %w", err)
	}
	if existing != nil && existing.ID != userID {
		return ErrEmailTaken
	}

	// Generate cryptographically secure token.
	tokenBytes := make([]byte, magicTokenBytes) // reuse same 32-byte constant
	if _, err := rand.Read(tokenBytes); err != nil {
		return fmt.Errorf("generate token: %w", err)
	}
	token := hex.EncodeToString(tokenBytes)

	now := time.Now()
	ect := &models.EmailChangeToken{
		ID:        token,
		UserID:    userID,
		NewEmail:  newEmail,
		ExpiresAt: now.Add(emailChangeTokenExpiry),
		CreatedAt: now,
	}
	if err := s.db.CreateEmailChangeToken(ect); err != nil {
		return fmt.Errorf("store email change token: %w", err)
	}

	// Send confirmation email to the NEW address.
	link := fmt.Sprintf("%s/auth/email-change?token=%s", baseURL, token)
	if err := s.mailer.SendEmailChangeVerification(newEmail, link); err != nil {
		return fmt.Errorf("send verification email: %w", err)
	}

	return nil
}

// ConfirmEmailChange consumes the token and updates the user's email address.
// Returns the userID of the affected account so the caller can refresh session context.
func (s *Service) ConfirmEmailChange(token string) (string, error) {
	ect, err := s.db.GetEmailChangeToken(token)
	if err != nil {
		return "", fmt.Errorf("get email change token: %w", err)
	}
	if ect == nil {
		return "", ErrInvalidEmailChangeToken
	}

	// Consume first — prevent double-use even if the update below fails.
	if err := s.db.ConsumeEmailChangeToken(token); err != nil {
		return "", fmt.Errorf("consume email change token: %w", err)
	}

	newHash := identity.EmailHash(ect.NewEmail)
	if err := s.db.UpdateUserEmail(ect.UserID, ect.NewEmail, newHash); err != nil {
		return "", fmt.Errorf("update user email: %w", err)
	}

	return ect.UserID, nil
}

// DeleteAccount deletes the user and all associated sessions/tokens.
// The caller should clear the session cookie after this returns.
func (s *Service) DeleteAccount(userID string) error {
	if err := s.db.DeleteUser(userID); err != nil {
		return fmt.Errorf("delete user: %w", err)
	}

	// Publish user.deleted event — non-fatal if it fails.
	s.publish("user.deleted", map[string]any{"user_id": userID})

	return nil
}
