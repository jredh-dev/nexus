package auth

import "errors"

// Authentication errors
var (
	// Token errors
	ErrInvalidToken     = errors.New("invalid token")
	ErrExpiredToken     = errors.New("token expired")
	ErrTokenNotFound    = errors.New("token not found")
	ErrTokenAlreadyUsed = errors.New("token already used")

	// User errors
	ErrUserNotFound         = errors.New("user not found")
	ErrUserAlreadyExists    = errors.New("user already exists")
	ErrInvalidCredentials   = errors.New("invalid credentials")
	ErrEmailNotVerified     = errors.New("email not verified")
	ErrEmailAlreadyVerified = errors.New("email already verified")

	// Invitation errors
	ErrInvitationNotFound    = errors.New("invitation not found")
	ErrInvitationExpired     = errors.New("invitation expired")
	ErrInvitationAlreadyUsed = errors.New("invitation already used")

	// Verification errors
	ErrVerificationNotFound = errors.New("verification not found")
	ErrVerificationExpired  = errors.New("verification expired")
	ErrVerificationFailed   = errors.New("verification failed")
	ErrInvalidCode          = errors.New("invalid verification code")
	ErrMaxAttemptsExceeded  = errors.New("max verification attempts exceeded")

	// Session errors
	ErrInvalidSession = errors.New("invalid session")
	ErrSessionExpired = errors.New("session expired")

	// General errors
	ErrUnauthorized = errors.New("unauthorized")
	ErrForbidden    = errors.New("forbidden")
)
