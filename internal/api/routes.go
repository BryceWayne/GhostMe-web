package api

import (
	"context"
	"log"
	"strings"
	"time"

	"github.com/BryceWayne/GhostMe-web/internal/chat"
	"github.com/gofiber/fiber/v2"
	"github.com/gofiber/template/html/v2"
	"github.com/gofiber/websocket/v2"
)

// SetupApp configures the Fiber app and routes
func SetupApp(server *chat.Server, viewsPath, publicPath string) *fiber.App {
	// Note: We avoid setting server.ViewsPath here to avoid side effects.
	// The server should be configured before passing it to SetupApp.

	engine := html.New(viewsPath, ".html")
	app := fiber.New(fiber.Config{Views: engine})

	app.Static("/static", publicPath)

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
		email, ok := token.Claims["email"].(string)
		if !ok || !strings.HasSuffix(email, "@gmail.com") {
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
	app.Get("/ws", websocket.New(server.HandleWebSocket))

	// Initial Route
	app.Get("/", func(c *fiber.Ctx) error {
		return c.Render("index", fiber.Map{"Title": "Secure Chat"})
	})

	return app
}
