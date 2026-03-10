// auth.go — GCP service account key → short-lived Bearer token.
// Used by all gcloud-mcp tools. Falls back to GCE metadata server when
// running inside GCP; uses SA key JWT exchange otherwise.
package tools

import (
	"crypto"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"crypto/x509"
	"encoding/base64"
	"encoding/json"
	"encoding/pem"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"time"
)

const tokenEndpoint = "https://oauth2.googleapis.com/token"

// saKey is the structure of a GCP service account JSON key file.
type saKey struct {
	Type                    string `json:"type"`
	ProjectID               string `json:"project_id"`
	PrivateKeyID            string `json:"private_key_id"`
	PrivateKey              string `json:"private_key"`
	ClientEmail             string `json:"client_email"`
	ClientID                string `json:"client_id"`
	AuthURI                 string `json:"auth_uri"`
	TokenURI                string `json:"token_uri"`
	AuthProviderX509CertURL string `json:"auth_provider_x509_cert_url"`
	ClientX509CertURL       string `json:"client_x509_cert_url"`
}

// getTokenFromSAKey exchanges a service account key file for a short-lived access token.
// It builds a signed JWT assertion and posts it to the OAuth2 token endpoint.
func getTokenFromSAKey(keyFile string) (string, error) {
	data, err := os.ReadFile(keyFile)
	if err != nil {
		return "", fmt.Errorf("read SA key file: %w", err)
	}

	var key saKey
	if err := json.Unmarshal(data, &key); err != nil {
		return "", fmt.Errorf("parse SA key: %w", err)
	}

	jwt, err := makeJWT(key)
	if err != nil {
		return "", fmt.Errorf("make JWT: %w", err)
	}

	return exchangeJWT(jwt)
}

// makeJWT creates a signed JWT assertion for the service account.
// Format: base64url(header) + "." + base64url(claims) + "." + base64url(signature)
func makeJWT(key saKey) (string, error) {
	now := time.Now().Unix()

	// JWT header — RS256 with the key ID from the SA key file.
	header := map[string]string{
		"alg": "RS256",
		"typ": "JWT",
		"kid": key.PrivateKeyID,
	}
	headerJSON, err := json.Marshal(header)
	if err != nil {
		return "", fmt.Errorf("marshal header: %w", err)
	}
	headerB64 := base64.RawURLEncoding.EncodeToString(headerJSON)

	// JWT claims — request full cloud-platform scope, valid for 1 hour.
	claims := map[string]any{
		"iss":   key.ClientEmail,
		"scope": "https://www.googleapis.com/auth/cloud-platform",
		"aud":   tokenEndpoint,
		"iat":   now,
		"exp":   now + 3600,
	}
	claimsJSON, err := json.Marshal(claims)
	if err != nil {
		return "", fmt.Errorf("marshal claims: %w", err)
	}
	claimsB64 := base64.RawURLEncoding.EncodeToString(claimsJSON)

	// The signing input is the two base64url-encoded segments joined by a dot.
	signingInput := headerB64 + "." + claimsB64

	// Decode the PEM-encoded RSA private key.
	block, _ := pem.Decode([]byte(key.PrivateKey))
	if block == nil {
		return "", fmt.Errorf("failed to decode PEM block from private key")
	}
	privateKey, err := x509.ParsePKCS8PrivateKey(block.Bytes)
	if err != nil {
		return "", fmt.Errorf("parse private key: %w", err)
	}
	rsaKey, ok := privateKey.(*rsa.PrivateKey)
	if !ok {
		return "", fmt.Errorf("private key is not RSA")
	}

	// Sign with RS256: SHA-256 hash + PKCS#1 v1.5 RSA signature.
	digest := sha256.Sum256([]byte(signingInput))
	sig, err := rsa.SignPKCS1v15(rand.Reader, rsaKey, crypto.SHA256, digest[:])
	if err != nil {
		return "", fmt.Errorf("sign JWT: %w", err)
	}

	return signingInput + "." + base64.RawURLEncoding.EncodeToString(sig), nil
}

// exchangeJWT posts a JWT assertion to the OAuth2 token endpoint and returns
// the short-lived access token string.
func exchangeJWT(jwt string) (string, error) {
	form := url.Values{
		"grant_type": {"urn:ietf:params:oauth:grant-type:jwt-bearer"},
		"assertion":  {jwt},
	}

	resp, err := http.PostForm(tokenEndpoint, form)
	if err != nil {
		return "", fmt.Errorf("token request: %w", err)
	}
	defer resp.Body.Close()

	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("token endpoint error (status %d): %s", resp.StatusCode, body)
	}

	var tok struct {
		AccessToken string `json:"access_token"`
		TokenType   string `json:"token_type"`
		ExpiresIn   int    `json:"expires_in"`
	}
	if err := json.Unmarshal(body, &tok); err != nil {
		return "", fmt.Errorf("parse token response: %w", err)
	}
	if tok.AccessToken == "" {
		return "", fmt.Errorf("empty access token in response: %s", body)
	}
	return tok.AccessToken, nil
}
