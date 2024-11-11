package main

import (
	"log"
	"net/http"
	"os"

	"hallertau/internal/auth"
	"hallertau/internal/core"
	"hallertau/internal/database"

	"github.com/gorilla/sessions"
)

func main() {
	// Recipes path must be a folder that exists, if it doesn't exist or is deleted after the
	// program starts, recipe changes will not be monitored.

	config := core.LoadConfig(os.Args[1])
	sessionStore := sessions.NewCookieStore([]byte(config.Server.SessionSecret))
	sessionStore.MaxAge(60 * 60 * 24)
	sessionStore.Options.Secure = true

	var state = core.State{
		DB:           database.Setup(),
		SessionStore: sessionStore,
		Config:       config,
	}
	defer state.DB.Close()

	state.LoadRecipes()
	go state.MonitorRecipesDirectory()

	serveMux := http.NewServeMux()

	fs := http.FileServer(http.Dir("static"))
	serveMux.Handle("/", fs)

	core.AddHandlers(serveMux, state)
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
