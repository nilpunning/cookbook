package core

import (
	"database/sql"
	"log"

	"github.com/BurntSushi/toml"
	"github.com/alexedwards/scs/v2"
)

type Config struct {
	Server struct {
		Address     string
		RecipesPath string
	}
	OIDC struct {
		Issuer       string
		ClientID     string
		ClientSecret string
		RedirectURI  string
	}
}

type State struct {
	DB             *sql.DB
	SessionManager *scs.SessionManager
	Config         Config
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
