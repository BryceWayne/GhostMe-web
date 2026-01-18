package chat

import (
	"context"
	"testing"
	"time"

	"firebase.google.com/go/v4/auth"
	"github.com/BryceWayne/MemoryStore/memorystore"
	"github.com/stretchr/testify/assert"
)

// MockVerifier for testing
type MockVerifier struct{}

func (m *MockVerifier) VerifyIDToken(ctx context.Context, idToken string) (*auth.Token, error) {
	return nil, nil
}

func TestNewServer(t *testing.T) {
	store := memorystore.NewMemoryStore()
	defer store.Stop()
	verifier := &MockVerifier{}

	server := NewServer(store, verifier)

	assert.NotNil(t, server)
	assert.Equal(t, store, server.Store)
	assert.Equal(t, verifier, server.Verifier)
	assert.NotNil(t, server.register)
	assert.NotNil(t, server.unregister)
	assert.Equal(t, "./views", server.ViewsPath)
}

func TestServer_BroadcastToLocalClients(t *testing.T) {
	// This test mainly verifies that it doesn't panic when no clients are connected
	store := memorystore.NewMemoryStore()
	defer store.Stop()
	verifier := &MockVerifier{}
	server := NewServer(store, verifier)

	// Should not panic
	server.broadcastToLocalClients("<div>Hello</div>")

	// To test with clients, we would need to mock websocket.Conn which is hard.
	// We'll rely on integration tests for that.
}

func TestServer_RunHub_Integration(t *testing.T) {
	store := memorystore.NewMemoryStore()
	defer store.Stop()
	verifier := &MockVerifier{}
	server := NewServer(store, verifier)

	// Start Hub
	go server.RunHub()

	// Wait for subscription
	time.Sleep(100 * time.Millisecond)

	// Publish a message
	server.Store.Publish("chat", []byte("test message"))

	// Since we have no clients, we can't verify receipt easily here without mocking internal state,
	// but we can ensure it doesn't crash.
	time.Sleep(50 * time.Millisecond)
}
