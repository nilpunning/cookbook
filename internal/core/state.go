package core

import (
	"log"
	"net/http"

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
		LLM            *string
	}
	Google *struct {
		APIKey *string
		Model  *string
	}
	Ollama *struct {
		ServerURL *string
		Model     *string
	}
	OpenAI *struct {
		Token   *string
		BaseURL *string
		Model   *string
	}
	OIDC *struct {
		Issuer             string
		EndSessionEndpoint string
		ClientID           string
		ClientSecret       string
		RedirectURI        string
		GroupsClaim        *[]string
	}
	FormBasedAuthUsers *map[string]string
}

type AuthInfo struct {
	MountPoint string
	LoginUrl   string
	LogoutUrl  string
}

func NewAuthInfo(mountPoint string) AuthInfo {
	return AuthInfo{
		MountPoint: mountPoint,
		LoginUrl:   mountPoint + "/login",
		LogoutUrl:  mountPoint + "/logout",
	}
}

type Auth struct {
	AuthInfo
	AddHandlers func(state State, mux *http.ServeMux)
}

func AddHandlersNop(state State, mux *http.ServeMux) {}

type State struct {
	Index        bleve.Index
	SessionStore *sessions.CookieStore
	Config       Config
	Auth         Auth
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
