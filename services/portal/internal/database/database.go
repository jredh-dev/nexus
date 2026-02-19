package database

import (
	"context"
	"fmt"
	"os"

	"cloud.google.com/go/firestore"
	firebase "firebase.google.com/go/v4"
	"firebase.google.com/go/v4/auth"
	"github.com/jredh-dev/nexus/services/portal/config"
	"google.golang.org/api/option"
)

// DB holds database connections
type DB struct {
	Firestore *firestore.Client
	Auth      *auth.Client
	ctx       context.Context
}

// New creates a new database connection
func New(ctx context.Context, cfg *config.Config) (*DB, error) {
	var opts []option.ClientOption

	// Configure emulator mode if enabled
	if cfg.Firebase.UseEmulator {
		// Set environment variables for Firebase emulators
		if cfg.Firebase.EmulatorAuthHost != "" {
			setEmulatorEnv("FIREBASE_AUTH_EMULATOR_HOST", cfg.Firebase.EmulatorAuthHost)
		}
		if cfg.Firebase.EmulatorFirestoreHost != "" {
			setEmulatorEnv("FIRESTORE_EMULATOR_HOST", cfg.Firebase.EmulatorFirestoreHost)
		}
		// Emulator doesn't require credentials
		opts = nil
	} else if cfg.Firebase.CredentialsPath != "" {
		// If credentials path is provided, use it (production mode)
		opts = append(opts, option.WithCredentialsFile(cfg.Firebase.CredentialsPath))
	}

	// Initialize Firebase app
	firebaseConfig := &firebase.Config{
		ProjectID: cfg.Firebase.ProjectID,
	}

	app, err := firebase.NewApp(ctx, firebaseConfig, opts...)
	if err != nil {
		return nil, fmt.Errorf("error initializing firebase app: %w", err)
	}

	// Initialize Firestore client
	firestoreClient, err := app.Firestore(ctx)
	if err != nil {
		return nil, fmt.Errorf("error initializing firestore: %w", err)
	}

	// Initialize Auth client
	authClient, err := app.Auth(ctx)
	if err != nil {
		return nil, fmt.Errorf("error initializing auth: %w", err)
	}

	return &DB{
		Firestore: firestoreClient,
		Auth:      authClient,
		ctx:       ctx,
	}, nil
}

// Close closes all database connections
func (db *DB) Close() error {
	if db.Firestore != nil {
		return db.Firestore.Close()
	}
	return nil
}

// Helper methods for common operations

// Collection returns a Firestore collection reference
func (db *DB) Collection(name string) *firestore.CollectionRef {
	return db.Firestore.Collection(name)
}

// Users returns the users collection
func (db *DB) Users() *firestore.CollectionRef {
	return db.Collection("users")
}

// Clients returns the clients collection
func (db *DB) Clients() *firestore.CollectionRef {
	return db.Collection("clients")
}

// Projects returns the projects collection
func (db *DB) Projects() *firestore.CollectionRef {
	return db.Collection("projects")
}

// Issues returns the issues collection
func (db *DB) Issues() *firestore.CollectionRef {
	return db.Collection("github_issues")
}

// IssueComments returns the issue comments collection
func (db *DB) IssueComments() *firestore.CollectionRef {
	return db.Collection("issue_comments")
}

// Invoices returns the invoices collection
func (db *DB) Invoices() *firestore.CollectionRef {
	return db.Collection("invoices")
}

// setEmulatorEnv sets an environment variable for Firebase emulator configuration
func setEmulatorEnv(key, value string) {
	if err := os.Setenv(key, value); err != nil {
		fmt.Printf("Warning: failed to set %s: %v\n", key, err)
	}
}
