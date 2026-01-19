package chat

import (
	"context"
	"net/http"
	"strings"
	"testing"
	"time"

	"firebase.google.com/go/v4/auth"
	"github.com/BryceWayne/GhostMe-web/internal/models"
	"github.com/BryceWayne/MemoryStore/memorystore"
	"github.com/fasthttp/websocket"
	"github.com/gofiber/fiber/v2"
	gofiberwebsocket "github.com/gofiber/websocket/v2"
	"github.com/stretchr/testify/assert"
)

// MockVerifier for testing
type MockVerifier struct{}

func (m *MockVerifier) VerifyIDToken(ctx context.Context, idToken string) (*auth.Token, error) {
	return &auth.Token{
		Claims: map[string]interface{}{
			"email": "test@example.com",
		},
	}, nil
}

// helper to add a test client
func addTestClient(s *Server) *models.Client {
	client := &models.Client{
		Email:       "test@example.com",
		DisplayName: "TestUser",
		Send:        make(chan []byte, 10),
	}
	s.clientsMu.Lock()
	s.clients[nil] = client
	s.clientsMu.Unlock()
	return client
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
	store := memorystore.NewMemoryStore()
	defer store.Stop()
	verifier := &MockVerifier{}
	server := NewServer(store, verifier)

	// Test 1: No clients (should not panic)
	server.broadcastToLocalClients("<div>Hello</div>")

	// Test 2: With a client
	client := addTestClient(server)

	msg := "<div>Hello World</div>"
	server.broadcastToLocalClients(msg)

	select {
	case received := <-client.Send:
		assert.Equal(t, msg, string(received))
	case <-time.After(100 * time.Millisecond):
		t.Fatal("Timeout waiting for message")
	}
}

func TestServer_RunHub_Delivery(t *testing.T) {
	store := memorystore.NewMemoryStore()
	defer store.Stop()
	verifier := &MockVerifier{}
	server := NewServer(store, verifier)

	// Manually add a client
	client := addTestClient(server)

	// Start Hub
	go server.RunHub()

	// Wait for subscription to be established
	time.Sleep(100 * time.Millisecond)

	// Publish a message to the store
	expectedMsg := "test message"
	server.Store.Publish("chat", []byte(expectedMsg))

	// Verify client receives it
	select {
	case received := <-client.Send:
		assert.Equal(t, expectedMsg, string(received))
	case <-time.After(500 * time.Millisecond):
		t.Fatal("Timeout waiting for message from hub")
	}
}

func TestServer_HandleWebSocket_Integration(t *testing.T) {
	// Setup Server
	store := memorystore.NewMemoryStore()
	defer store.Stop()
	verifier := &MockVerifier{}
	server := NewServer(store, verifier)
	// Point to a directory that exists, e.g. ../../views if running from root, or use absolute.
	// We will try to rely on "views" folder if it exists in root, otherwise we might need to mock template loading or ensure path is correct.
	// Since we run tests from root, "./views" should exist.
	server.ViewsPath = "../../views" // Assumes running from internal/chat, adjust if needed.
	// Wait, "go test ./..." usually sets CWD to the package directory.
	// So if in internal/chat, we need ../../views.

	// Start Hub
	go server.RunHub()

	// Setup Fiber App
	app := fiber.New()

	// Middleware to mock authentication and set Locals
	app.Use("/ws", func(c *fiber.Ctx) error {
		c.Locals("email", "test@example.com")
		if gofiberwebsocket.IsWebSocketUpgrade(c) {
			return c.Next()
		}
		return fiber.ErrUpgradeRequired
	})

	app.Get("/ws", gofiberwebsocket.New(server.HandleWebSocket))

	// Start App in Goroutine
	go func() {
		app.Listen(":9998")
	}()
	time.Sleep(200 * time.Millisecond) // Wait for start
	defer app.Shutdown()

	// Client Connect
	dialer := websocket.Dialer{
		HandshakeTimeout: 5 * time.Second,
	}
	conn, _, err := dialer.Dial("ws://localhost:9998/ws", http.Header{})
	if err != nil {
		t.Fatalf("Failed to dial: %v", err)
	}
	defer conn.Close()

	// Send Message
	msg := `{"text": "Integration Test :ghost:"}`
	if err := conn.WriteMessage(websocket.TextMessage, []byte(msg)); err != nil {
		t.Fatalf("Failed to write: %v", err)
	}

	// Read Response
	conn.SetReadDeadline(time.Now().Add(2 * time.Second))
	_, p, err := conn.ReadMessage()
	if err != nil {
		t.Fatalf("Failed to read: %v", err)
	}

	response := string(p)
	// Check if GhostMe translation worked
	if !strings.Contains(response, "Integration Test 👻") {
		t.Errorf("Expected translated message, got: %s", response)
	}

	// Check if template rendering worked (look for HTML tags if using template)
	// If template failed to load, it falls back to raw text, so checking content is safer.
}
