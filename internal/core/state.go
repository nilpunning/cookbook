package core

import (
	"log"

	"github.com/BurntSushi/toml"
	"github.com/blevesearch/bleve/v2"
	"github.com/gorilla/sessions"
)

var Version = "dev"

type Config struct {
	Server struct {
		Address        string
		RecipesPath    string
		SessionSecrets []string
		CSRFKey        string
		Language       string
		SecureCookies  bool
	}
	OIDC *struct {
		Issuer             string
		EndSessionEndpoint string
		ClientID           string
		ClientSecret       string
		RedirectURI        string
	}
	FormBasedAuthUsers *map[string]string
}

type State struct {
	Index        bleve.Index
	SessionStore *sessions.CookieStore
	Config       Config
}

func LoadConfig(path string) Config {
	config := Config{}
	config.Server.SecureCookies = true
	_, err := toml.DecodeFile(path, &config)
	if err != nil {
		log.Fatal(err)
	}

	// log.Printf("%+v", config)

	return config
}
