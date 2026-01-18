package main

import (
	"bytes"
	"context"
	"log"
	"mime"
	"os"
	"strings"
	"sync"
	"text/template"
	"time"

	"github.com/BryceWayne/MemoryStore/memorystore"
	firebase "firebase.google.com/go/v4"
	"github.com/gofiber/fiber/v2"
	"github.com/gofiber/template/html/v2"
	"github.com/gofiber/websocket/v2"
	"google.golang.org/api/option"
)

// --- Structs ---
type Client struct {
	Email       string // REAL Identity (for logging)
	DisplayName string // FAKE Identity (for UI)
	mu          sync.Mutex
}

type MessageData struct {
	DisplayName string
	Content     string
	Timestamp   string
	IsMine      bool // Helper to style "my" messages vs "others"
}

type Server struct {
	Store      *memorystore.MemoryStore
	Verifier   Verifier
	clients    map[*websocket.Conn]*Client
	register   chan *websocket.Conn
	unregister chan *websocket.Conn
	clientsMu  sync.RWMutex
}

func NewServer(store *memorystore.MemoryStore, verifier Verifier) *Server {
	return &Server{
		Store:      store,
		Verifier:   verifier,
		clients:    make(map[*websocket.Conn]*Client),
		register:   make(chan *websocket.Conn),
		unregister: make(chan *websocket.Conn),
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
		go func(conn *websocket.Conn, client *Client) {
			client.mu.Lock()
			defer client.mu.Unlock()
			// Fire and forget
			conn.WriteMessage(websocket.TextMessage, []byte(html))
		}(connection, c)
	}
}

// SetupApp configures the Fiber app and routes
func SetupApp(server *Server) *fiber.App {
	engine := html.New("./views", ".html")
	app := fiber.New(fiber.Config{Views: engine})

	app.Static("/static", "./public")

	// 1. Login Handler
	app.Post("/login", func(c *fiber.Ctx) error {
		type LoginRequest struct {
			IDToken string `json:"idToken" form:"idToken"`
		}

		var req LoginRequest
		if err := c.BodyParser(&req); err != nil {
			log.Println("LOGIN ERROR:", err)
			return c.Status(400).SendString("Bad Request: " + err.Error())
		}

		token, err := server.Verifier.VerifyIDToken(context.Background(), req.IDToken)
		if err != nil {
			return c.Status(401).SendString("Invalid Token")
		}

		// Check Claims
		email, _ := token.Claims["email"].(string)
		if !strings.HasSuffix(email, "@gmail.com") {
			return c.Status(403).SendString("Access Denied: @gmail.com accounts only.")
		}

		// Set HTTP-Only Cookie
		c.Cookie(&fiber.Cookie{
			Name:     "session_user",
			Value:    email,
			HTTPOnly: true,
			Expires:  time.Now().Add(24 * time.Hour),
			SameSite: "Strict",
		})

		return c.Render("chat", nil)
	})

 	// 2. WebSocket Middleware (Protects /ws)
    // UPDATED: Supports both Web Cookies AND Mobile Auth Headers
    app.Use("/ws", func(c *fiber.Ctx) error {
        // Source A: Try to get email from Cookie (Web Client)
        email := c.Cookies("session_user")

        // Source B: If no cookie, try Authorization Header (Mobile Client)
        if email == "" {
            authHeader := c.Get("Authorization")
            if strings.HasPrefix(authHeader, "Bearer ") {
                idToken := strings.TrimPrefix(authHeader, "Bearer ")
                
                // Verify the token on the fly
                token, err := server.Verifier.VerifyIDToken(context.Background(), idToken)
                if err == nil {
                    // Success! Extract email from token
                    if claimsEmail, ok := token.Claims["email"].(string); ok {
                        email = claimsEmail
                    }
                } else {
                    log.Printf("Mobile Auth Failed: %v", err)
                }
            }
        }

        // Final Check: Did we find a valid email from either source?
        if email == "" {
            return c.Status(401).SendString("Unauthorized: No valid cookie or token found")
        }

        c.Locals("email", email)
        
        if websocket.IsWebSocketUpgrade(c) {
            return c.Next()
        }
        return fiber.ErrUpgradeRequired
    })

	// 3. WebSocket Handler
	app.Get("/ws", websocket.New(func(c *websocket.Conn) {
		email := c.Locals("email").(string)

		client := &Client{
			Email:       email,
			DisplayName: "Anonymous",
		}

		server.clientsMu.Lock()
		server.clients[c] = client
		server.clientsMu.Unlock()

		server.register <- c

		defer func() {
			server.unregister <- c
			c.Close()
		}()

		// Pre-parse template
		// Note: This assumes CWD is root. In tests, might need adjustment if not running from root.
		tmpl, _ := template.ParseFiles("views/message.html")

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

			msgData := MessageData{
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

			server.Store.Publish("chat", tpl.Bytes())
		}
	}))

	// Initial Route
	app.Get("/", func(c *fiber.Ctx) error {
		return c.Render("index", fiber.Map{"Title": "Secure Chat"})
	})

	return app
}

func initFirebase() *firebase.App {
	var opts []option.ClientOption

	if _, err := os.Stat("serviceAccountKey.json"); err == nil {
		log.Println("Using local serviceAccountKey.json")
		opts = append(opts, option.WithCredentialsFile("serviceAccountKey.json"))
	} else {
		log.Println("Using Application Default Credentials (Cloud Run)")
	}

	projectID := os.Getenv("GOOGLE_CLOUD_PROJECT")
	if projectID == "" {
		projectID = "ghostme-34590"
		log.Printf("GOOGLE_CLOUD_PROJECT not set. Using fallback: %s", projectID)
	}

	config := &firebase.Config{ProjectID: projectID}

	app, err := firebase.NewApp(context.Background(), config, opts...)
	if err != nil {
		log.Fatalf("Error initializing Firebase: %v", err)
	}
	return app
}

func main() {
	mime.AddExtensionType(".js", "application/javascript")

	firebaseApp := initFirebase()
	verifier, err := NewFirebaseVerifier(firebaseApp)
	if err != nil {
		log.Fatal(err)
	}

	var store *memorystore.MemoryStore
	projectID := os.Getenv("GOOGLE_CLOUD_PROJECT")
	if projectID != "" {
		log.Println("Using GCP PubSub")
		store = memorystore.NewMemoryStoreWithConfig(memorystore.Config{GCPProjectID: projectID})
	} else {
		log.Println("Using In-Memory PubSub")
		store = memorystore.NewMemoryStore()
	}
	defer store.Stop()

	server := NewServer(store, verifier)
	go server.RunHub()

	app := SetupApp(server)

	port := os.Getenv("PORT")
	if port == "" {
		port = "8080"
	}
	log.Fatal(app.Listen(":" + port))
}
