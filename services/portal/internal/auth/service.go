package auth

import (
	"context"
	"fmt"
	"math/rand"
	"time"

	"cloud.google.com/go/firestore"
	"firebase.google.com/go/v4/auth"
	"github.com/google/uuid"
	"github.com/jredh-dev/nexus/services/portal/config"
	"github.com/jredh-dev/nexus/services/portal/internal/database"
	"github.com/jredh-dev/nexus/services/portal/pkg/models"
	"google.golang.org/api/iterator"
)

// Service handles authentication operations
type Service struct {
	db  *database.DB
	cfg *config.Config
}

// New creates a new auth service
func New(db *database.DB, cfg *config.Config) *Service {
	return &Service{
		db:  db,
		cfg: cfg,
	}
}

// generateToken generates a new UUID v4 token
func generateToken() (string, error) {
	token, err := uuid.NewRandom()
	if err != nil {
		return "", fmt.Errorf("failed to generate token: %w", err)
	}
	return token.String(), nil
}

// generateSMSCode generates a random 6-digit code for SMS verification
func generateSMSCode() string {
	return fmt.Sprintf("%06d", rand.Intn(1000000))
}

// CreateInvitation creates a new invitation for a user
func (s *Service) CreateInvitation(ctx context.Context, email, role, clientID, createdBy string) (*models.Invitation, error) {
	// Validate role
	if role != "admin" && role != "consultant" && role != "client" {
		return nil, fmt.Errorf("invalid role: %s", role)
	}

	// Generate token
	token, err := generateToken()
	if err != nil {
		return nil, err
	}

	// Create invitation
	now := time.Now()
	expiresAt := now.AddDate(0, 0, s.cfg.Auth.InviteExpirationDays)

	invitation := &models.Invitation{
		ID:        uuid.New().String(),
		Email:     email,
		Token:     token,
		Role:      role,
		ClientID:  clientID,
		CreatedBy: createdBy,
		Status:    "pending",
		ExpiresAt: expiresAt,
		CreatedAt: now,
		UsedAt:    nil,
	}

	// Save to Firestore
	_, err = s.db.Collection("invitations").Doc(invitation.ID).Set(ctx, invitation)
	if err != nil {
		return nil, fmt.Errorf("failed to create invitation: %w", err)
	}

	return invitation, nil
}

// ValidateInvitation validates an invitation token and returns the invitation
func (s *Service) ValidateInvitation(ctx context.Context, token string) (*models.Invitation, error) {
	// Query for invitation by token
	iter := s.db.Collection("invitations").
		Where("token", "==", token).
		Where("status", "==", "pending").
		Documents(ctx)

	doc, err := iter.Next()
	if err == iterator.Done {
		return nil, ErrInvitationNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("failed to query invitation: %w", err)
	}

	var invitation models.Invitation
	if err := doc.DataTo(&invitation); err != nil {
		return nil, fmt.Errorf("failed to parse invitation: %w", err)
	}

	// Check if expired
	if time.Now().After(invitation.ExpiresAt) {
		// Update status to expired
		_, err = s.db.Collection("invitations").Doc(invitation.ID).Update(ctx, []firestore.Update{
			{Path: "status", Value: "expired"},
		})
		if err != nil {
			return nil, fmt.Errorf("failed to update invitation status: %w", err)
		}
		return nil, ErrInvitationExpired
	}

	// Check if already used
	if invitation.UsedAt != nil {
		return nil, ErrInvitationAlreadyUsed
	}

	return &invitation, nil
}

// MarkInvitationUsed marks an invitation as used
func (s *Service) MarkInvitationUsed(ctx context.Context, invitationID string) error {
	now := time.Now()
	_, err := s.db.Collection("invitations").Doc(invitationID).Update(ctx, []firestore.Update{
		{Path: "status", Value: "accepted"},
		{Path: "used_at", Value: now},
	})
	if err != nil {
		return fmt.Errorf("failed to mark invitation as used: %w", err)
	}
	return nil
}

// SendEmailVerification creates an email verification record
func (s *Service) SendEmailVerification(ctx context.Context, userID, email string) (*models.EmailVerification, error) {
	// Generate token
	token, err := generateToken()
	if err != nil {
		return nil, err
	}

	// Create verification record
	now := time.Now()
	expiresAt := now.Add(time.Duration(s.cfg.Auth.VerificationExpirationHours) * time.Hour)

	verification := &models.EmailVerification{
		ID:        uuid.New().String(),
		UserID:    userID,
		Email:     email,
		Token:     token,
		Status:    "pending",
		ExpiresAt: expiresAt,
		CreatedAt: now,
	}

	// Save to Firestore
	_, err = s.db.Collection("email_verifications").Doc(verification.ID).Set(ctx, verification)
	if err != nil {
		return nil, fmt.Errorf("failed to create email verification: %w", err)
	}

	// In mock mode, just return the verification
	// In production, this would send an actual email
	if s.cfg.Auth.MockVerificationMode {
		// Mock mode: verification is automatically successful
		return verification, nil
	}

	// TODO: Send actual email with verification link
	// emailLink := fmt.Sprintf("https://example.com/verify-email?token=%s", token)

	return verification, nil
}

// VerifyEmail verifies an email using the provided token
func (s *Service) VerifyEmail(ctx context.Context, token string) error {
	// Query for verification by token
	iter := s.db.Collection("email_verifications").
		Where("token", "==", token).
		Where("status", "==", "pending").
		Documents(ctx)

	doc, err := iter.Next()
	if err == iterator.Done {
		return ErrVerificationNotFound
	}
	if err != nil {
		return fmt.Errorf("failed to query verification: %w", err)
	}

	var verification models.EmailVerification
	if err := doc.DataTo(&verification); err != nil {
		return fmt.Errorf("failed to parse verification: %w", err)
	}

	// Check if expired
	if time.Now().After(verification.ExpiresAt) {
		// Update status to expired
		_, err = s.db.Collection("email_verifications").Doc(verification.ID).Update(ctx, []firestore.Update{
			{Path: "status", Value: "expired"},
		})
		if err != nil {
			return fmt.Errorf("failed to update verification status: %w", err)
		}
		return ErrVerificationExpired
	}

	// Mark verification as verified
	now := time.Now()
	_, err = s.db.Collection("email_verifications").Doc(verification.ID).Update(ctx, []firestore.Update{
		{Path: "status", Value: "verified"},
		{Path: "verified_at", Value: now},
	})
	if err != nil {
		return fmt.Errorf("failed to update verification: %w", err)
	}

	// Update user's email_verified status
	_, err = s.db.Users().Doc(verification.UserID).Update(ctx, []firestore.Update{
		{Path: "email_verified", Value: true},
		{Path: "updated_at", Value: now},
	})
	if err != nil {
		return fmt.Errorf("failed to update user: %w", err)
	}

	return nil
}

// SendSMSVerification creates an SMS verification record
func (s *Service) SendSMSVerification(ctx context.Context, userID, phoneNumber string) (*models.SMSVerification, error) {
	// Generate 6-digit code
	code := generateSMSCode()

	// Create verification record
	now := time.Now()
	expiresAt := now.Add(time.Duration(s.cfg.Auth.SMSExpirationMinutes) * time.Minute)

	verification := &models.SMSVerification{
		ID:           uuid.New().String(),
		UserID:       userID,
		PhoneNumber:  phoneNumber,
		Code:         code,
		Status:       "pending",
		ExpiresAt:    expiresAt,
		CreatedAt:    now,
		AttemptsLeft: s.cfg.Auth.SMSMaxAttempts,
	}

	// Save to Firestore
	_, err := s.db.Collection("sms_verifications").Doc(verification.ID).Set(ctx, verification)
	if err != nil {
		return nil, fmt.Errorf("failed to create SMS verification: %w", err)
	}

	// In mock mode, just return the verification with the code visible
	// In production, this would send an actual SMS
	if s.cfg.Auth.MockVerificationMode {
		// Mock mode: code is returned for testing
		return verification, nil
	}

	// TODO: Send actual SMS with verification code
	// smsService.Send(phoneNumber, fmt.Sprintf("Your verification code is: %s", code))

	return verification, nil
}

// VerifySMS verifies an SMS code
func (s *Service) VerifySMS(ctx context.Context, userID, code string) error {
	// Query for latest pending verification for this user
	iter := s.db.Collection("sms_verifications").
		Where("user_id", "==", userID).
		Where("status", "==", "pending").
		OrderBy("created_at", firestore.Desc).
		Limit(1).
		Documents(ctx)

	doc, err := iter.Next()
	if err == iterator.Done {
		return ErrVerificationNotFound
	}
	if err != nil {
		return fmt.Errorf("failed to query SMS verification: %w", err)
	}

	var verification models.SMSVerification
	if err := doc.DataTo(&verification); err != nil {
		return fmt.Errorf("failed to parse verification: %w", err)
	}

	// Check if expired
	if time.Now().After(verification.ExpiresAt) {
		_, err = s.db.Collection("sms_verifications").Doc(verification.ID).Update(ctx, []firestore.Update{
			{Path: "status", Value: "expired"},
		})
		if err != nil {
			return fmt.Errorf("failed to update verification status: %w", err)
		}
		return ErrVerificationExpired
	}

	// Check attempts left
	if verification.AttemptsLeft <= 0 {
		return ErrMaxAttemptsExceeded
	}

	// Verify code
	if verification.Code != code {
		// Decrement attempts
		newAttempts := verification.AttemptsLeft - 1
		updates := []firestore.Update{
			{Path: "attempts_left", Value: newAttempts},
		}

		// If no attempts left, mark as failed
		if newAttempts <= 0 {
			updates = append(updates, firestore.Update{Path: "status", Value: "failed"})
		}

		_, err = s.db.Collection("sms_verifications").Doc(verification.ID).Update(ctx, updates)
		if err != nil {
			return fmt.Errorf("failed to update verification attempts: %w", err)
		}

		if newAttempts <= 0 {
			return ErrMaxAttemptsExceeded
		}
		return ErrInvalidCode
	}

	// Mark verification as verified
	now := time.Now()
	_, err = s.db.Collection("sms_verifications").Doc(verification.ID).Update(ctx, []firestore.Update{
		{Path: "status", Value: "verified"},
		{Path: "verified_at", Value: now},
	})
	if err != nil {
		return fmt.Errorf("failed to update verification: %w", err)
	}

	// Update user's phone_verified status
	_, err = s.db.Users().Doc(verification.UserID).Update(ctx, []firestore.Update{
		{Path: "phone_verified", Value: true},
		{Path: "phone_number", Value: verification.PhoneNumber},
		{Path: "updated_at", Value: now},
	})
	if err != nil {
		return fmt.Errorf("failed to update user: %w", err)
	}

	return nil
}

// CreateUser creates a new user in Firebase Auth and Firestore
func (s *Service) CreateUser(ctx context.Context, email, password, name, role, clientID string) (*models.User, error) {
	// Validate role
	if role != "admin" && role != "consultant" && role != "client" {
		return nil, fmt.Errorf("invalid role: %s", role)
	}

	// Create user in Firebase Auth
	params := (&auth.UserToCreate{}).
		Email(email).
		EmailVerified(false).
		Password(password).
		DisplayName(name).
		Disabled(false)

	firebaseUser, err := s.db.Auth.CreateUser(ctx, params)
	if err != nil {
		return nil, fmt.Errorf("failed to create Firebase user: %w", err)
	}

	// Create user record in Firestore
	now := time.Now()
	user := &models.User{
		ID:               firebaseUser.UID,
		Email:            email,
		Name:             name,
		Role:             role,
		ClientID:         clientID,
		EmailVerified:    false,
		PhoneVerified:    false,
		TwoFactorEnabled: false,
		CreatedAt:        now,
		UpdatedAt:        now,
	}

	_, err = s.db.Users().Doc(user.ID).Set(ctx, user)
	if err != nil {
		// Rollback: delete Firebase user if Firestore creation fails
		_ = s.db.Auth.DeleteUser(ctx, firebaseUser.UID)
		return nil, fmt.Errorf("failed to create user in Firestore: %w", err)
	}

	return user, nil
}

// Login authenticates a user and creates a session cookie
func (s *Service) Login(ctx context.Context, email, password string, rememberMe bool) (string, error) {
	// Note: Firebase Admin SDK doesn't provide password verification directly
	// In production, this would use Firebase Client SDK or custom tokens
	// For now, we'll look up the user and create a session cookie

	// Query for user by email
	iter := s.db.Users().Where("email", "==", email).Limit(1).Documents(ctx)
	doc, err := iter.Next()
	if err == iterator.Done {
		return "", ErrInvalidCredentials
	}
	if err != nil {
		return "", fmt.Errorf("failed to query user: %w", err)
	}

	var user models.User
	if err := doc.DataTo(&user); err != nil {
		return "", fmt.Errorf("failed to parse user: %w", err)
	}

	// Check if email verification is required
	if s.cfg.Auth.RequireEmailVerification && !user.EmailVerified {
		return "", ErrEmailNotVerified
	}

	// Create custom token for the user
	// Note: Firebase Admin SDK doesn't directly verify passwords
	// In production flow:
	// 1. Server creates custom token with user.ID
	// 2. Client exchanges custom token for ID token via Firebase Client SDK (with password)
	// 3. Client sends ID token back to server
	// 4. Server creates session cookie from ID token with appropriate expiration
	//    based on rememberMe flag (1 hour vs 1 week)

	// For now, we return the custom token as a placeholder
	// The handler layer will implement the full OAuth flow
	token, err := s.db.Auth.CustomToken(ctx, user.ID)
	if err != nil {
		return "", fmt.Errorf("failed to create custom token: %w", err)
	}

	// Update last login time
	now := time.Now()
	_, err = s.db.Users().Doc(user.ID).Update(ctx, []firestore.Update{
		{Path: "last_login_at", Value: now},
		{Path: "updated_at", Value: now},
	})
	if err != nil {
		return "", fmt.Errorf("failed to update user login time: %w", err)
	}

	return token, nil
}

// VerifySessionCookie verifies a session cookie and returns the token
func (s *Service) VerifySessionCookie(ctx context.Context, sessionCookie string) (*auth.Token, error) {
	// Verify the session cookie
	token, err := s.db.Auth.VerifySessionCookie(ctx, sessionCookie)
	if err != nil {
		return nil, ErrInvalidSession
	}

	return token, nil
}

// RevokeSession revokes all refresh tokens for a user
func (s *Service) RevokeSession(ctx context.Context, userID string) error {
	err := s.db.Auth.RevokeRefreshTokens(ctx, userID)
	if err != nil {
		return fmt.Errorf("failed to revoke session: %w", err)
	}
	return nil
}

// GetUserByID retrieves a user by their ID
func (s *Service) GetUserByID(ctx context.Context, userID string) (*models.User, error) {
	doc, err := s.db.Users().Doc(userID).Get(ctx)
	if err != nil {
		return nil, ErrUserNotFound
	}

	var user models.User
	if err := doc.DataTo(&user); err != nil {
		return nil, fmt.Errorf("failed to parse user: %w", err)
	}

	return &user, nil
}

// GetUserByEmail retrieves a user by their email
func (s *Service) GetUserByEmail(ctx context.Context, email string) (*models.User, error) {
	iter := s.db.Users().Where("email", "==", email).Limit(1).Documents(ctx)
	doc, err := iter.Next()
	if err == iterator.Done {
		return nil, ErrUserNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("failed to query user: %w", err)
	}

	var user models.User
	if err := doc.DataTo(&user); err != nil {
		return nil, fmt.Errorf("failed to parse user: %w", err)
	}

	return &user, nil
}
