package main

import (
	"context"
	"net/http/httptest"
	"strings"
	"testing"

	"firebase.google.com/go/v4/auth"
	"github.com/BryceWayne/MemoryStore/memorystore"
	"github.com/gofiber/fiber/v2"
)

type MockVerifier struct {
	ShouldFail bool
	Email      string
}

func (m *MockVerifier) VerifyIDToken(ctx context.Context, idToken string) (*auth.Token, error) {
	if m.ShouldFail {
		return nil, fiber.NewError(fiber.StatusUnauthorized, "Invalid Token")
	}
	return &auth.Token{
		Claims: map[string]interface{}{
			"email": m.Email,
		},
	}, nil
}

func TestIndex(t *testing.T) {
	store := memorystore.NewMemoryStore()
	defer store.Stop()
	verifier := &MockVerifier{}
	server := NewServer(store, verifier)
	app := SetupApp(server)

	req := httptest.NewRequest("GET", "/", nil)
	resp, err := app.Test(req)
	if err != nil {
		t.Fatalf("Failed to test request: %v", err)
	}

	if resp.StatusCode != fiber.StatusOK {
		t.Errorf("Expected status 200, got %d", resp.StatusCode)
	}
}

func TestLoginSuccess(t *testing.T) {
	store := memorystore.NewMemoryStore()
	defer store.Stop()
	verifier := &MockVerifier{Email: "test@gmail.com"}
	server := NewServer(store, verifier)
	app := SetupApp(server)

	body := strings.NewReader(`{"idToken": "valid_token"}`)
	req := httptest.NewRequest("POST", "/login", body)
	req.Header.Set("Content-Type", "application/json")

	resp, err := app.Test(req)
	if err != nil {
		t.Fatalf("Failed to test request: %v", err)
	}

	if resp.StatusCode != fiber.StatusOK {
		t.Errorf("Expected status 200, got %d", resp.StatusCode)
	}

	// Check for cookie
	cookies := resp.Header["Set-Cookie"]
	found := false
	for _, c := range cookies {
		if strings.Contains(c, "session_user") {
			found = true
			break
		}
	}
	if !found {
		t.Error("Expected session_user cookie")
	}
}

func TestLoginInvalidEmail(t *testing.T) {
	store := memorystore.NewMemoryStore()
	defer store.Stop()
	verifier := &MockVerifier{Email: "test@other.com"}
	server := NewServer(store, verifier)
	app := SetupApp(server)

	body := strings.NewReader(`{"idToken": "valid_token"}`)
	req := httptest.NewRequest("POST", "/login", body)
	req.Header.Set("Content-Type", "application/json")

	resp, err := app.Test(req)
	if err != nil {
		t.Fatalf("Failed to test request: %v", err)
	}

	if resp.StatusCode != fiber.StatusForbidden {
		t.Errorf("Expected status 403, got %d", resp.StatusCode)
	}
}

func TestLoginFailure(t *testing.T) {
	store := memorystore.NewMemoryStore()
	defer store.Stop()
	verifier := &MockVerifier{ShouldFail: true}
	server := NewServer(store, verifier)
	app := SetupApp(server)

	body := strings.NewReader(`{"idToken": "invalid_token"}`)
	req := httptest.NewRequest("POST", "/login", body)
	req.Header.Set("Content-Type", "application/json")

	resp, err := app.Test(req)
	if err != nil {
		t.Fatalf("Failed to test request: %v", err)
	}

	if resp.StatusCode != fiber.StatusUnauthorized {
		t.Errorf("Expected status 401, got %d", resp.StatusCode)
	}
}

func TestWebSocketUnauthorized(t *testing.T) {
	store := memorystore.NewMemoryStore()
	defer store.Stop()
	verifier := &MockVerifier{}
	server := NewServer(store, verifier)
	app := SetupApp(server)

	req := httptest.NewRequest("GET", "/ws", nil)
	// No cookie set

	resp, err := app.Test(req)
	if err != nil {
		t.Fatalf("Failed to test request: %v", err)
	}

	if resp.StatusCode != fiber.StatusUnauthorized {
		t.Errorf("Expected status 401, got %d", resp.StatusCode)
	}
}
