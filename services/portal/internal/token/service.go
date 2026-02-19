package token

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"time"

	"firebase.google.com/go/v4/auth"
	"github.com/golang-jwt/jwt/v4"
)

// Service handles JWT token generation and validation for microservices
type Service struct {
	signingKey []byte
	issuer     string
	authClient *auth.Client
}

// Claims represents JWT claims for portal tokens
type Claims struct {
	UserID string   `json:"uid"`
	Email  string   `json:"email"`
	Roles  []string `json:"roles,omitempty"`
	jwt.RegisteredClaims
}

// ValidationResponse is returned by the validation endpoint
type ValidationResponse struct {
	Valid  bool     `json:"valid"`
	UserID string   `json:"user_id,omitempty"`
	Email  string   `json:"email,omitempty"`
	Roles  []string `json:"roles,omitempty"`
	Error  string   `json:"error,omitempty"`
}

// New creates a new token service
func New(signingKey string, issuer string, authClient *auth.Client) *Service {
	return &Service{
		signingKey: []byte(signingKey),
		issuer:     issuer,
		authClient: authClient,
	}
}

// GenerateSigningKey generates a secure random signing key
func GenerateSigningKey() (string, error) {
	bytes := make([]byte, 32)
	if _, err := rand.Read(bytes); err != nil {
		return "", fmt.Errorf("failed to generate signing key: %w", err)
	}
	return hex.EncodeToString(bytes), nil
}

// GenerateToken creates a JWT token for an authenticated user
func (s *Service) GenerateToken(userID, email string, roles []string, expiresIn time.Duration) (string, error) {
	now := time.Now()
	claims := Claims{
		UserID: userID,
		Email:  email,
		Roles:  roles,
		RegisteredClaims: jwt.RegisteredClaims{
			Issuer:    s.issuer,
			Subject:   userID,
			IssuedAt:  jwt.NewNumericDate(now),
			ExpiresAt: jwt.NewNumericDate(now.Add(expiresIn)),
			NotBefore: jwt.NewNumericDate(now),
		},
	}

	token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	return token.SignedString(s.signingKey)
}

// ValidateToken validates a JWT token and returns the claims
func (s *Service) ValidateToken(tokenString string) (*Claims, error) {
	token, err := jwt.ParseWithClaims(tokenString, &Claims{}, func(token *jwt.Token) (interface{}, error) {
		// Verify signing method
		if _, ok := token.Method.(*jwt.SigningMethodHMAC); !ok {
			return nil, fmt.Errorf("unexpected signing method: %v", token.Header["alg"])
		}
		return s.signingKey, nil
	})

	if err != nil {
		return nil, fmt.Errorf("failed to parse token: %w", err)
	}

	if claims, ok := token.Claims.(*Claims); ok && token.Valid {
		return claims, nil
	}

	return nil, fmt.Errorf("invalid token")
}

// ValidateFirebaseToken validates a Firebase ID token and returns user info
// This is used to verify the user is actually authenticated with Firebase
func (s *Service) ValidateFirebaseToken(ctx context.Context, idToken string) (*auth.Token, error) {
	token, err := s.authClient.VerifyIDToken(ctx, idToken)
	if err != nil {
		return nil, fmt.Errorf("invalid Firebase token: %w", err)
	}
	return token, nil
}
