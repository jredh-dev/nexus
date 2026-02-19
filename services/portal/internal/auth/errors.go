package auth

import "errors"

var (
	ErrInvalidCredentials = errors.New("invalid credentials")
	ErrUserNotFound       = errors.New("user not found")
	ErrSessionExpired     = errors.New("session expired")
	ErrUnauthorized       = errors.New("unauthorized")
	ErrEmailTaken         = errors.New("an account with this email already exists")
	ErrPhoneTaken         = errors.New("an account with this phone number already exists")
	ErrUsernameTaken      = errors.New("this username is already taken")
)
