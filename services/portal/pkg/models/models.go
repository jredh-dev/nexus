package models

import "time"

// User represents a user in the system
type User struct {
	ID               string     `firestore:"id" json:"id"`
	Email            string     `firestore:"email" json:"email"`
	Name             string     `firestore:"name" json:"name"`
	Role             string     `firestore:"role" json:"role"` // "admin", "consultant", "client"
	ClientID         string     `firestore:"client_id,omitempty" json:"client_id,omitempty"`
	EmailVerified    bool       `firestore:"email_verified" json:"email_verified"`
	PhoneNumber      string     `firestore:"phone_number,omitempty" json:"phone_number,omitempty"`
	PhoneVerified    bool       `firestore:"phone_verified" json:"phone_verified"`
	TwoFactorEnabled bool       `firestore:"two_factor_enabled" json:"two_factor_enabled"`
	CreatedAt        time.Time  `firestore:"created_at" json:"created_at"`
	UpdatedAt        time.Time  `firestore:"updated_at" json:"updated_at"`
	LastLoginAt      *time.Time `firestore:"last_login_at,omitempty" json:"last_login_at,omitempty"`
}

// Client represents a consulting client
type Client struct {
	ID        string    `firestore:"id" json:"id"`
	Name      string    `firestore:"name" json:"name"`
	Email     string    `firestore:"email" json:"email"`
	Status    string    `firestore:"status" json:"status"` // "active", "inactive", "suspended"
	CreatedAt time.Time `firestore:"created_at" json:"created_at"`
	UpdatedAt time.Time `firestore:"updated_at" json:"updated_at"`
}

// Project represents a client project
type Project struct {
	ID           string    `firestore:"id" json:"id"`
	ClientID     string    `firestore:"client_id" json:"client_id"`
	Name         string    `firestore:"name" json:"name"`
	Description  string    `firestore:"description" json:"description"`
	Status       string    `firestore:"status" json:"status"` // "active", "completed", "on_hold"
	GitHubRepo   string    `firestore:"github_repo" json:"github_repo"`
	SlackChannel string    `firestore:"slack_channel" json:"slack_channel,omitempty"`
	CreatedAt    time.Time `firestore:"created_at" json:"created_at"`
	UpdatedAt    time.Time `firestore:"updated_at" json:"updated_at"`
}

// GitHubIssue represents a cached GitHub issue
type GitHubIssue struct {
	ID           int64     `firestore:"id" json:"id"`
	Number       int       `firestore:"number" json:"number"`
	ProjectID    string    `firestore:"project_id" json:"project_id"`
	Title        string    `firestore:"title" json:"title"`
	Body         string    `firestore:"body" json:"body"`
	State        string    `firestore:"state" json:"state"` // "open", "closed"
	Labels       []string  `firestore:"labels" json:"labels"`
	Assignees    []string  `firestore:"assignees" json:"assignees"`
	URL          string    `firestore:"url" json:"url"`
	CreatedAt    time.Time `firestore:"created_at" json:"created_at"`
	UpdatedAt    time.Time `firestore:"updated_at" json:"updated_at"`
	LastSyncedAt time.Time `firestore:"last_synced_at" json:"last_synced_at"`
}

// IssueComment represents a comment on an issue
type IssueComment struct {
	ID        string    `firestore:"id" json:"id"`
	IssueID   int       `firestore:"issue_id" json:"issue_id"`
	UserID    string    `firestore:"user_id" json:"user_id"`
	Body      string    `firestore:"body" json:"body"`
	CreatedAt time.Time `firestore:"created_at" json:"created_at"`
}

// Invoice represents a billing invoice
type Invoice struct {
	ID              string     `firestore:"id" json:"id"`
	ClientID        string     `firestore:"client_id" json:"client_id"`
	Amount          int64      `firestore:"amount" json:"amount"` // in cents
	Currency        string     `firestore:"currency" json:"currency"`
	Status          string     `firestore:"status" json:"status"` // "draft", "sent", "paid", "overdue"
	Description     string     `firestore:"description" json:"description"`
	DueDate         time.Time  `firestore:"due_date" json:"due_date"`
	PaidAt          *time.Time `firestore:"paid_at,omitempty" json:"paid_at,omitempty"`
	StripeInvoiceID string     `firestore:"stripe_invoice_id,omitempty" json:"stripe_invoice_id,omitempty"`
	CreatedAt       time.Time  `firestore:"created_at" json:"created_at"`
	UpdatedAt       time.Time  `firestore:"updated_at" json:"updated_at"`
}

// Invitation represents an invite to join the portal
type Invitation struct {
	ID        string     `firestore:"id" json:"id"`
	Email     string     `firestore:"email" json:"email"`
	Token     string     `firestore:"token" json:"token"` // UUID v4
	Role      string     `firestore:"role" json:"role"`   // "client", "consultant", "admin"
	ClientID  string     `firestore:"client_id,omitempty" json:"client_id,omitempty"`
	CreatedBy string     `firestore:"created_by" json:"created_by"` // Admin user ID
	Status    string     `firestore:"status" json:"status"`         // "pending", "accepted", "expired"
	ExpiresAt time.Time  `firestore:"expires_at" json:"expires_at"` // Default: 7 days
	CreatedAt time.Time  `firestore:"created_at" json:"created_at"`
	UsedAt    *time.Time `firestore:"used_at,omitempty" json:"used_at,omitempty"`
}

// EmailVerification represents a pending email verification
type EmailVerification struct {
	ID         string     `firestore:"id" json:"id"`
	UserID     string     `firestore:"user_id" json:"user_id"`
	Email      string     `firestore:"email" json:"email"`
	Token      string     `firestore:"token" json:"token"`           // UUID v4
	Status     string     `firestore:"status" json:"status"`         // "pending", "verified", "expired"
	ExpiresAt  time.Time  `firestore:"expires_at" json:"expires_at"` // Default: 24 hours
	CreatedAt  time.Time  `firestore:"created_at" json:"created_at"`
	VerifiedAt *time.Time `firestore:"verified_at,omitempty" json:"verified_at,omitempty"`
}

// SMSVerification represents a pending SMS verification (for 2FA)
type SMSVerification struct {
	ID           string     `firestore:"id" json:"id"`
	UserID       string     `firestore:"user_id" json:"user_id"`
	PhoneNumber  string     `firestore:"phone_number" json:"phone_number"`
	Code         string     `firestore:"code" json:"code"`             // 6-digit code
	Status       string     `firestore:"status" json:"status"`         // "pending", "verified", "expired", "failed"
	ExpiresAt    time.Time  `firestore:"expires_at" json:"expires_at"` // Default: 10 minutes
	CreatedAt    time.Time  `firestore:"created_at" json:"created_at"`
	VerifiedAt   *time.Time `firestore:"verified_at,omitempty" json:"verified_at,omitempty"`
	AttemptsLeft int        `firestore:"attempts_left" json:"attempts_left"` // Default: 3 attempts
}
