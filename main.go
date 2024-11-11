package main

import (
	"log"
	"net/http"
	"os"

	"hallertau/internal/auth"
	"hallertau/internal/core"
	"hallertau/internal/database"
	"hallertau/internal/handlers"
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

	handlers.AddHandlers(serveMux, state)
	auth.AddOIDCAuth(serveMux, state, "/auth/oidc")

	log.Println("Server starting on", state.Config.Server.Address)
	err := http.ListenAndServe(
		state.Config.Server.Address,
		serveMux,
	)
	if err != nil {
		log.Fatal(err)
	}
}
