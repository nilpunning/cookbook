package core

import (
	"database/sql"
	"log"

	"github.com/BurntSushi/toml"
	"github.com/gorilla/sessions"
)

type Config struct {
	Server struct {
		Address       string
		RecipesPath   string
		SessionSecret string
		CSRFKey       string
	}
	OIDC struct {
		Issuer             string
		EndSessionEndpoint string
		ClientID           string
		ClientSecret       string
		RedirectURI        string
	}
}

type State struct {
	DB           *sql.DB
	SessionStore *sessions.CookieStore
	Config       Config
}

func LoadConfig(path string) Config {
	config := Config{}
	_, err := toml.DecodeFile(path, &config)
	if err != nil {
		log.Fatal(err)
	}

	// log.Printf("%+v", config)

	return config
}
