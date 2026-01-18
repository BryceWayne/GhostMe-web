package main

import (
	"context"

	firebase "firebase.google.com/go/v4"
	"firebase.google.com/go/v4/auth"
)

// Verifier defines the interface for ID token verification
type Verifier interface {
	VerifyIDToken(ctx context.Context, idToken string) (*auth.Token, error)
}

// FirebaseVerifier implements Verifier using the Firebase Admin SDK
type FirebaseVerifier struct {
	client *auth.Client
}

// NewFirebaseVerifier creates a new FirebaseVerifier from a Firebase App
func NewFirebaseVerifier(app *firebase.App) (*FirebaseVerifier, error) {
	client, err := app.Auth(context.Background())
	if err != nil {
		return nil, err
	}
	return &FirebaseVerifier{client: client}, nil
}

// VerifyIDToken verifies the given ID token
func (v *FirebaseVerifier) VerifyIDToken(ctx context.Context, idToken string) (*auth.Token, error) {
	return v.client.VerifyIDToken(ctx, idToken)
}
