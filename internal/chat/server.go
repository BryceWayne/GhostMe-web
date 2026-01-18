package chat

import (
	"bytes"
	"log"
	"strings"
	"sync"
	"path/filepath"
	"text/template"
	"time"

	"github.com/BryceWayne/GhostMe-web/internal/auth"
	"github.com/BryceWayne/GhostMe-web/internal/models"
	"github.com/BryceWayne/MemoryStore/memorystore"
	"github.com/gofiber/websocket/v2"
)

type Server struct {
	Store      *memorystore.MemoryStore
	Verifier   auth.Verifier
	clients    map[*websocket.Conn]*models.Client
	register   chan *websocket.Conn
	unregister chan *websocket.Conn
	clientsMu  sync.RWMutex
	ViewsPath  string
}

func NewServer(store *memorystore.MemoryStore, verifier auth.Verifier) *Server {
	return &Server{
		Store:      store,
		Verifier:   verifier,
		clients:    make(map[*websocket.Conn]*models.Client),
		register:   make(chan *websocket.Conn),
		unregister: make(chan *websocket.Conn),
		ViewsPath:  "./views",
	}
}

func (s *Server) RunHub() {
	// Subscribe to "chat" topic
	msgs, err := s.Store.Subscribe("chat")
	if err != nil {
		log.Fatal(err)
	}

	// Fan-out routine
	go func() {
		for msgBytes := range msgs {
			s.broadcastToLocalClients(string(msgBytes))
		}
	}()

	// Connection Management Routine
	for {
		select {
		case connection := <-s.register:
			// Handled in WS handler for now to minimize changes,
			// but effectively we just log or track count here.
			_ = connection
		case connection := <-s.unregister:
			s.clientsMu.Lock()
			delete(s.clients, connection)
			s.clientsMu.Unlock()
		}
	}
}

func (s *Server) broadcastToLocalClients(html string) {
	s.clientsMu.RLock()
	defer s.clientsMu.RUnlock()
	for connection, c := range s.clients {
		go func(conn *websocket.Conn, client *models.Client) {
			client.Lock()
			defer client.Unlock()
			// Fire and forget
			conn.WriteMessage(websocket.TextMessage, []byte(html))
		}(connection, c)
	}
}

func (s *Server) HandleWebSocket(c *websocket.Conn) {
	email, ok := c.Locals("email").(string)
	if !ok {
		// Should not happen if middleware does its job, but safe check
		log.Println("WebSocket connection missing email in locals")
		c.Close()
		return
	}

	client := &models.Client{
		Email:       email,
		DisplayName: "Anonymous",
	}

	s.clientsMu.Lock()
	s.clients[c] = client
	s.clientsMu.Unlock()

	s.register <- c

	defer func() {
		s.unregister <- c
		c.Close()
	}()

	// Pre-parse template
	// Note: This assumes CWD is root. In tests, might need adjustment if not running from root.
	tmpl, err := template.ParseFiles(filepath.Join(s.ViewsPath, "message.html"))
	if err != nil {
		log.Printf("Error parsing template: %v", err)
	}

	for {
		type Payload struct {
			Text string `json:"text"`
		}
		var p Payload
		if err := c.ReadJSON(&p); err != nil {
			break
		}

		// 1. REJECT EMPTY VOID MESSAGES
		if strings.TrimSpace(p.Text) == "" {
			continue
		}

		// PARSE EMOJIS (The Ghost Translation Layer) ---
		p.Text = strings.ReplaceAll(p.Text, ":ghost:", "👻")

		// --- AUDIT LOGGING ---
		log.Printf("[AUDIT] User: %s | Message: %s", client.Email, p.Text)

		msgData := models.MessageData{
			DisplayName: client.DisplayName,
			Content:     p.Text,
			Timestamp:   time.Now().Format("15:04"),
		}

		var tpl bytes.Buffer
		// Check if tmpl is nil (e.g. file not found)
		if tmpl != nil {
			if err := tmpl.Execute(&tpl, msgData); err != nil {
				continue
			}
		} else {
			// Fallback if template loading failed (e.g. in tests)
			tpl.WriteString(p.Text)
		}

		s.Store.Publish("chat", tpl.Bytes())
	}
}
