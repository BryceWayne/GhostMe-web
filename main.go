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

// --- Global State ---
var (
	store       *memorystore.MemoryStore
	firebaseApp *firebase.App
	clients     = make(map[*websocket.Conn]*Client)
	register    = make(chan *websocket.Conn)
	unregister  = make(chan *websocket.Conn)
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

// --- Initialization ---
func initFirebase() {
    var opts []option.ClientOption
    
    // Only use the file if it exists locally
    if _, err := os.Stat("serviceAccountKey.json"); err == nil {
        log.Println("Using local serviceAccountKey.json")
        opts = append(opts, option.WithCredentialsFile("serviceAccountKey.json"))
    } else {
        log.Println("Using Application Default Credentials (Cloud Run)")
    }

    // If opts is empty, Firebase automatically looks for Cloud Run credentials!
    app, err := firebase.NewApp(context.Background(), nil, opts...)
    if err != nil {
        log.Fatalf("Error initializing Firebase: %v", err)
    }
    firebaseApp = app
}

// --- The Hub (Using MemoryStore) ---
func runHub() {
	// Subscribe to "chat" topic (Works for Local AND Cloud PubSub)
	msgs, err := store.Subscribe("chat")
	if err != nil {
		log.Fatal(err)
	}

	// Fan-out routine: Listen to PubSub -> Send to Local Clients
	go func() {
		for msgBytes := range msgs {
			broadcastToLocalClients(string(msgBytes))
		}
	}()

	// Connection Management Routine
	for {
		select {
		case connection := <-register:
			// In a real app, track active user count here
			_ = connection
		case connection := <-unregister:
			delete(clients, connection)
		}
	}
}

func broadcastToLocalClients(html string) {
	for connection, c := range clients {
		go func(conn *websocket.Conn, client *Client) {
			client.mu.Lock()
			defer client.mu.Unlock()
			// Fire and forget
			conn.WriteMessage(websocket.TextMessage, []byte(html))
		}(connection, c)
	}
}

// --- Main ---
func main() {
	// This forces Go to use the correct header, ignoring the Alpine OS defaults
    mime.AddExtensionType(".js", "application/javascript")

	initFirebase()

	// Initialize MemoryStore (Cloud vs Local detection)
	projectID := os.Getenv("GOOGLE_CLOUD_PROJECT")
	if projectID != "" {
		log.Println("Using GCP PubSub")
		store = memorystore.NewMemoryStoreWithConfig(memorystore.Config{GCPProjectID: projectID})
	} else {
		log.Println("Using In-Memory PubSub")
		store = memorystore.NewMemoryStore()
	}
	defer store.Stop()

	// Setup View Engine
	engine := html.New("./views", ".html")
	app := fiber.New(fiber.Config{Views: engine})

	// ---------------------------------------------------------
    // !!! CRITICAL FIX: ENABLE STATIC FILE SERVING !!!
    // This tells Fiber: "When someone asks for /static, look in ./public"
    // ---------------------------------------------------------
    app.Static("/static", "./public")

	// 1. Login Handler (Exchanges Google ID Token for Session Cookie)
	app.Post("/login", func(c *fiber.Ctx) error {
		type LoginRequest struct {
            // Add 'form' tag so it works even if sent as form data
            IDToken string `json:"idToken" form:"idToken"`
        }
        
		var req LoginRequest
		// NEW DEBUG CODE:
		if err := c.BodyParser(&req); err != nil {
			log.Println("LOGIN ERROR:", err) // Print error to terminal
			log.Println("RAW BODY:", string(c.Body())) // Print what we actually received
			return c.Status(400).SendString("Bad Request: " + err.Error())
		}

		// Verify Token via Firebase Admin SDK
		client, err := firebaseApp.Auth(context.Background())
		if err != nil {
			return c.Status(500).SendString("Auth Error")
		}
		
		token, err := client.VerifyIDToken(context.Background(), req.IDToken)
		if err != nil {
			return c.Status(401).SendString("Invalid Token")
		}

		// Check Claims
		email, _ := token.Claims["email"].(string)
		if !strings.HasSuffix(email, "@gmail.com") {
			return c.Status(403).SendString("Access Denied: @gmail.com accounts only.")
		}

		// Set HTTP-Only Cookie (Secure!)
		c.Cookie(&fiber.Cookie{
			Name:     "session_user",
			Value:    email, // In prod: Encrypt this value!
			HTTPOnly: true,
			Expires:  time.Now().Add(24 * time.Hour),
			SameSite: "Strict",
		})

		// Return the Chat Interface (HTMX Swap)
		return c.Render("chat", nil)
	})

	// 2. WebSocket Middleware (Protects /ws)
	app.Use("/ws", func(c *fiber.Ctx) error {
		email := c.Cookies("session_user")
		if email == "" {
			return c.Status(401).SendString("Unauthorized")
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
		
		// Create Client State
		client := &Client{
			Email:       email,
			DisplayName: "Anonymous", // The mask
		}

		clients[c] = client
		register <- c
		
		defer func() {
			unregister <- c
			c.Close()
		}()

		// Pre-parse template
		tmpl, _ := template.ParseFiles("views/message.html")

		for {
			type Payload struct {
				Text string `json:"text"`
			}
			var p Payload
			if err := c.ReadJSON(&p); err != nil {
				break
			}

			// --- AUDIT LOGGING (Real Identity) ---
			log.Printf("[AUDIT] User: %s | Message: %s", client.Email, p.Text)

			// --- PUBLIC BROADCAST (Anonymous) ---
			msgData := MessageData{
				DisplayName: client.DisplayName,
				Content:     p.Text,
				Timestamp:   time.Now().Format("15:04"),
			}

			var tpl bytes.Buffer
			if err := tmpl.Execute(&tpl, msgData); err != nil {
				continue
			}

			// Publish to MemoryStore (Cloud or Local)
			store.Publish("chat", tpl.Bytes())
		}
	}))

	// Initial Route
	app.Get("/", func(c *fiber.Ctx) error {
		return c.Render("index", fiber.Map{"Title": "Secure Chat"})
	})

	go runHub()

	port := os.Getenv("PORT")
	if port == "" { port = "8080" }
	log.Fatal(app.Listen(":" + port))
}