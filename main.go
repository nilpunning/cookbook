package main

import (
	"encoding/base64"
	"flag"
	"log"
	"net/http"
	"syscall"

	"hallertau/internal/auth"
	"hallertau/internal/core"
	"hallertau/internal/handlers"
	"hallertau/internal/search"

	"github.com/gorilla/csrf"
	"github.com/gorilla/securecookie"
	"golang.org/x/term"
)

func serve(configPath string) {
	cfg := core.LoadConfig(configPath)

	var state = core.State{
		Index:        search.NewIndex(cfg.Server.Language),
		SessionStore: auth.NewSessionStore(cfg.Server.SessionSecrets, cfg.Server.SecureCookies),
		Config:       cfg,
	}
	defer state.Index.Close()

	state.LoadRecipes()
	go state.MonitorRecipesDirectory()

	serveMux := http.NewServeMux()

	fs := http.FileServer(http.Dir("static"))
	serveMux.Handle("/", fs)

	var loginURL, logoutURL string
	if cfg.OIDC != nil {
		loginURL, logoutURL = auth.AddOIDCAuth(serveMux, state, "/auth/oidc")
	} else {
		if cfg.FormBasedAuthUsers != nil {
			loginURL, logoutURL = auth.AddFormBasedAuth(serveMux, state, "/auth/form")
		}
	}
	handlers.AddHandlers(serveMux, state, loginURL, logoutURL)

	csrfMiddleware := csrf.Protect(
		[]byte(state.Config.Server.CSRFKey),
		csrf.Secure(cfg.Server.SecureCookies),
	)

	log.Println("Server starting on", state.Config.Server.Address)
	err := http.ListenAndServe(
		state.Config.Server.Address,
		csrfMiddleware(serveMux),
	)
	if err != nil {
		log.Fatal(err)
	}
}

func main() {
	// Recipes path must be a folder that exists, if it doesn't exist or is deleted after the
	// program starts, recipe changes will not be monitored.

	log.SetFlags(log.LstdFlags | log.Lshortfile)

	// Parse command-line arguments
	configPath := flag.String("c", "", "Start server with config toml file. Ex. -c config.toml")
	password_hash := flag.Bool("p", false, "Hash password for form based authentication.")
	help := flag.Bool("h", false, "Print help.")
	key := flag.Bool("k", false, "Generates a new key which can be used for secrets in config.")
	flag.Parse()

	if *help {
		flag.Usage()
		return
	}

	if *password_hash {
		log.Println("Enter password:")
		b, err := term.ReadPassword(int(syscall.Stdin))
		if err != nil {
			panic(err)
		}
		log.Println(base64.RawStdEncoding.EncodeToString(b))
		return
	}

	if *key {
		b := securecookie.GenerateRandomKey(32)
		log.Println(base64.RawStdEncoding.EncodeToString(b))
		return
	}

	if *configPath != "" {
		serve(*configPath)
		return
	}

	flag.Usage()
}
