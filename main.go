package main

import (
	"encoding/hex"
	"flag"
	"fmt"
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

	csrfKey, err := hex.DecodeString(state.Config.Server.CSRFKey)
	if err != nil {
		log.Fatal("cannot read CSRFKey config")
	}

	csrfMiddleware := csrf.Protect(
		csrfKey,
		csrf.Secure(cfg.Server.SecureCookies),
	)

	log.Println("Server starting on", state.Config.Server.Address)
	err = http.ListenAndServe(
		state.Config.Server.Address,
		csrfMiddleware(serveMux),
	)
	if err != nil {
		log.Fatal(err)
	}
}

func main() {
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
		fmt.Println("Enter password:")
		b, err := term.ReadPassword(int(syscall.Stdin))
		if err != nil {
			panic(err)
		}
		fmt.Println(auth.HashPassword(b))
		return
	}

	if *key {
		b := securecookie.GenerateRandomKey(32)
		fmt.Printf("%x\n", b)
		return
	}

	if *configPath != "" {
		serve(*configPath)
		return
	}

	flag.Usage()
}
