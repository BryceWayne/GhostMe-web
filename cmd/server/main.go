package main

import (
	"context"
	"log"
	"mime"
	"os"

	"github.com/BryceWayne/GhostMe-web/internal/api"
	"github.com/BryceWayne/GhostMe-web/internal/auth"
	"github.com/BryceWayne/GhostMe-web/internal/chat"
	"github.com/BryceWayne/MemoryStore/memorystore"

	firebase "firebase.google.com/go/v4"
	"google.golang.org/api/option"
)

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
	verifier, err := auth.NewFirebaseVerifier(firebaseApp)
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

	server := chat.NewServer(store, verifier)
	server.ViewsPath = "./views"
	go server.RunHub()

	app := api.SetupApp(server, "./views", "./public")

	port := os.Getenv("PORT")
	if port == "" {
		port = "8080"
	}
	log.Fatal(app.Listen(":" + port))
}
