package auth

import (
	"net/http"

	"github.com/gorilla/sessions"
)

var sessionKey = "session"

func NewSessionStore(secret string) *sessions.CookieStore {
	store := sessions.NewCookieStore([]byte(secret))
	store.MaxAge(60 * 60 * 24)
	store.Options.Secure = true
	store.Options.HttpOnly = true
	return store
}

func NewSession(store *sessions.CookieStore, r *http.Request) (*sessions.Session, error) {
	return store.New(r, sessionKey)
}

func GetSession(store *sessions.CookieStore, r *http.Request) (*sessions.Session, error) {
	return store.Get(r, sessionKey)
}

func IsAuthenticated(store *sessions.CookieStore, r *http.Request) (bool, error) {
	session, err := GetSession(store, r)
	if err != nil {
		return false, err
	}
	return session.Values["sub"] != nil, nil
}
