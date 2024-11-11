package auth

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"hallertau/internal/core"
	"io"
	"log"
	"net/http"
	"net/url"
	"time"

	"github.com/coreos/go-oidc/v3/oidc"
	"golang.org/x/oauth2"
)

func randString(nByte int) (string, error) {
	b := make([]byte, nByte)
	if _, err := io.ReadFull(rand.Reader, b); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(b), nil
}

func setRandomCookie(w http.ResponseWriter, name string) string {
	randString, err := randString(16)
	if err != nil {
		http.Error(w, "Internal error", http.StatusInternalServerError)
		return ""
	}
	c := &http.Cookie{
		Name:     name,
		Value:    randString,
		MaxAge:   int(time.Hour.Seconds()),
		Secure:   true,
		HttpOnly: true,
	}
	http.SetCookie(w, c)
	return randString
}

func AddOIDCAuth(serveMux *http.ServeMux, state core.State, mountPoint string) {
	ctx := context.Background()

	provider, err := oidc.NewProvider(ctx, state.Config.OIDC.Issuer)
	if err != nil {
		log.Fatal(err)
	}

	verifier := provider.Verifier(&oidc.Config{
		ClientID: state.Config.OIDC.ClientID,
	})

	config := oauth2.Config{
		ClientID:     state.Config.OIDC.ClientID,
		ClientSecret: state.Config.OIDC.ClientSecret,
		Endpoint:     provider.Endpoint(),
		RedirectURL:  state.Config.OIDC.RedirectURI,
		Scopes:       []string{oidc.ScopeOpenID, "profile", "email"},
	}

	serveMux.HandleFunc(mountPoint+"/", func(w http.ResponseWriter, r *http.Request) {
		cookieValue := setRandomCookie(w, "state")
		nonceValue := setRandomCookie(w, "nonce")

		err := ClearSession(state.SessionStore, r, w)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		http.Redirect(w, r, config.AuthCodeURL(cookieValue, oidc.Nonce(nonceValue)), http.StatusFound)
	})

	serveMux.HandleFunc(mountPoint+"/callback", func(w http.ResponseWriter, r *http.Request) {
		stateCookie, err := r.Cookie("state")
		if err != nil {
			http.Error(w, "state not found", http.StatusBadRequest)
			return
		}
		if r.URL.Query().Get("state") != stateCookie.Value {
			http.Error(w, "state did not match", http.StatusBadRequest)
			return
		}
		oauth2Token, err := config.Exchange(ctx, r.URL.Query().Get("code"))
		if err != nil {
			http.Error(w, "Failed to exchange token: "+err.Error(), http.StatusInternalServerError)
			return
		}
		rawIDToken, ok := oauth2Token.Extra("id_token").(string)
		if !ok {
			http.Error(w, "No id_token field in oauth2 token.", http.StatusInternalServerError)
			return
		}
		idToken, err := verifier.Verify(ctx, rawIDToken)
		if err != nil {
			http.Error(w, "Failed to verify ID Token: "+err.Error(), http.StatusInternalServerError)
			return
		}
		nonce, err := r.Cookie("nonce")
		if err != nil {
			http.Error(w, "nonce not found", http.StatusBadRequest)
			return
		}
		if idToken.Nonce != nonce.Value {
			http.Error(w, "nonce did not match", http.StatusBadRequest)
			return
		}

		resp := struct {
			// OAuth2Token   *oauth2.Token
			IDTokenClaims *json.RawMessage
		}{
			// oauth2Token,
			new(json.RawMessage),
		}

		if err := idToken.Claims(&resp.IDTokenClaims); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}

		// data, err := json.MarshalIndent(resp, "", "    ")
		// if err != nil {
		// 	http.Error(w, err.Error(), http.StatusInternalServerError)
		// 	return
		// }
		// log.Println(string(data))

		var claims struct {
			Sub string `json:"sub"`
		}
		if err := json.Unmarshal(*resp.IDTokenClaims, &claims); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}

		session, err := GetSession(state.SessionStore, r)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		session.Values["sub"] = claims.Sub
		err = session.Save(r, w)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}

		http.Redirect(w, r, "/", http.StatusSeeOther)
	})

	serveMux.HandleFunc(mountPoint+"/logout", func(w http.ResponseWriter, r *http.Request) {
		err := ClearSession(state.SessionStore, r, w)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}

		logoutURL, err := url.Parse(state.Config.OIDC.EndSessionEndpoint)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		q := logoutURL.Query()
		q.Set("post_logout_redirect_uri", "/")
		logoutURL.RawQuery = q.Encode()

		log.Println("Logout URL:", logoutURL.String())

		http.Redirect(w, r, logoutURL.String(), http.StatusSeeOther)
	})
}
