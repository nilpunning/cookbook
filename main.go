package main

import (
	"log"
	"net/http"
	"os"

	"hallertau/internal/auth"
	"hallertau/internal/core"
	"hallertau/internal/database"
	"hallertau/internal/handlers"

	"github.com/gorilla/csrf"
)

func main() {
	// Recipes path must be a folder that exists, if it doesn't exist or is deleted after the
	// program starts, recipe changes will not be monitored.

	cfg := core.LoadConfig(os.Args[1])

	var state = core.State{
		DB:           database.Setup(),
		SessionStore: auth.NewSessionStore(cfg.Server.SessionSecret),
		Config:       cfg,
	}
	defer state.DB.Close()

	state.LoadRecipes()
	go state.MonitorRecipesDirectory()

	serveMux := http.NewServeMux()

	fs := http.FileServer(http.Dir("static"))
	serveMux.Handle("/", fs)

	loginURL := "/auth/oidc"
	auth.AddOIDCAuth(serveMux, state, loginURL)
	handlers.AddHandlers(serveMux, state, loginURL, "/auth/oidc/logout")

	csrfMiddleware := csrf.Protect([]byte(state.Config.Server.CSRFKey))

	log.Println("Server starting on", state.Config.Server.Address)
	err := http.ListenAndServe(
		state.Config.Server.Address,
		csrfMiddleware(serveMux),
	)
	if err != nil {
		log.Fatal(err)
	}
}
