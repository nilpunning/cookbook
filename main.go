package main

import (
	"encoding/hex"
	"flag"
	"fmt"
	"log"
	"net/http"
	"syscall"

	"cookbook/internal/auth"
	"cookbook/internal/core"
	"cookbook/internal/handlers"
	"cookbook/internal/search"

	"github.com/gorilla/csrf"
	"github.com/gorilla/securecookie"
	"golang.org/x/term"
)

func serve(configPath string) {
	cfg := core.LoadConfig(configPath)

	var authentication core.Auth
	if cfg.OIDC != nil {
		authentication = core.Auth{
			AuthInfo:    core.NewAuthInfo("/auth/oidc"),
			AddHandlers: auth.AddOIDCBasedHandlers,
		}
	} else if cfg.FormBasedAuthUsers != nil {
		authentication = core.Auth{
			AuthInfo:    core.NewAuthInfo("/auth/form"),
			AddHandlers: auth.AddFormBasedHandlers,
		}
	} else {
		authentication = core.Auth{AddHandlers: core.AddHandlersNop}
	}

	var state = core.State{
		Index:        search.NewIndex(cfg.Server.Language),
		SessionStore: auth.NewSessionStore(cfg.Server.SessionSecrets, cfg.Server.SecureCookies),
		Config:       cfg,
		Auth:         authentication,
	}
	defer state.Index.Close()

	state.LoadRecipes()
	go state.MonitorRecipesDirectory()

	serveMux := http.NewServeMux()

	fs := http.FileServer(http.Dir("static"))
	serveMux.Handle("/", fs)

	authentication.AddHandlers(state, serveMux)
	handlers.AddHandlers(state, serveMux)

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
	passwordHash := flag.Bool("p", false, "Hash password for form based authentication.")
	help := flag.Bool("h", false, "Print help.")
	key := flag.Bool("k", false, "Generates a new key which can be used for secrets in config.")
	flag.Parse()

	if *help {
		flag.Usage()
		return
	}

	if *passwordHash {
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
