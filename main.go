package main

import (
	"log"
	"net/http"
	"os"

	"hallertau/internal/auth"
	"hallertau/internal/core"
	"hallertau/internal/database"

	"github.com/alexedwards/scs/v2"
	"github.com/alexedwards/scs/v2/memstore"
)

func main() {
	// Recipes path must be a folder that exists, if it doesn't exist or is deleted after the
	// program starts, recipe changes will not be monitored.
	var state = core.State{
		DB:             database.Setup(),
		SessionManager: scs.New(),
		Config:         core.LoadConfig(os.Args[1]),
	}
	defer state.DB.Close()

	state.SessionManager.Store = memstore.New()

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
		state.SessionManager.LoadAndSave(serveMux),
	)
	if err != nil {
		log.Fatal(err)
	}
}
