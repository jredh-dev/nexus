package auth

import "errors"

var (
	ErrInvalidCredentials = errors.New("invalid credentials")
	ErrUserNotFound       = errors.New("user not found")
	ErrSessionExpired     = errors.New("session expired")
	ErrUnauthorized       = errors.New("unauthorized")
)
