package api

import (
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/BryceWayne/GhostMe-web/internal/chat"
	"github.com/BryceWayne/MemoryStore/memorystore"
	"github.com/fasthttp/websocket"
)

func TestWebSocketFlow(t *testing.T) {
	// Setup
	store := memorystore.NewMemoryStore()
	defer store.Stop()
	verifier := &MockVerifier{Email: "test@gmail.com"}
	server := chat.NewServer(store, verifier)
	server.ViewsPath = "../../views"

	// Start Hub
	go server.RunHub()

	app := SetupApp(server, "../../views", "../../public")

	// Start Server in a goroutine
	go func() {
		app.Listen(":9999")
	}()
	// Give it a moment to start
	time.Sleep(200 * time.Millisecond)
	defer app.Shutdown()

	// Connect via WebSocket
	// We need to simulate the cookie
	dialer := websocket.Dialer{
		HandshakeTimeout: 45 * time.Second,
	}

	header := make(http.Header)
	header.Add("Cookie", "session_user=test@gmail.com")

	conn, _, err := dialer.Dial("ws://localhost:9999/ws", header)
	if err != nil {
		t.Fatalf("Failed to dial: %v", err)
	}
	defer conn.Close()

	// Send a message
	msg := `{"text": "Hello World :ghost:"}`
	if err := conn.WriteMessage(websocket.TextMessage, []byte(msg)); err != nil {
		t.Fatalf("Failed to write: %v", err)
	}

	// Read response (should be the broadcasted HTML)
	_, p, err := conn.ReadMessage()
	if err != nil {
		t.Fatalf("Failed to read: %v", err)
	}

	response := string(p)
	if !strings.Contains(response, "Hello World 👻") {
		t.Errorf("Expected translated message, got: %s", response)
	}
}
