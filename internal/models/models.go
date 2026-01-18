package models

import "sync"

// Client represents a connected user
type Client struct {
	Email       string      // REAL Identity (for logging)
	DisplayName string      // FAKE Identity (for UI)
	Send        chan []byte // Buffered channel of outbound messages.
	mu          sync.Mutex
}

// Lock acquires the client's mutex
func (c *Client) Lock() {
	c.mu.Lock()
}

// Unlock releases the client's mutex
func (c *Client) Unlock() {
	c.mu.Unlock()
}

// MessageData holds data for rendering a message
type MessageData struct {
	DisplayName string
	Content     string
	Timestamp   string
	IsMine      bool // Helper to style "my" messages vs "others"
}
