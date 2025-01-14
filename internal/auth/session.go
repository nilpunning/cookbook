package auth

import (
	"log"
	"net/http"

	"github.com/gorilla/sessions"
)

var sessionKey = "session"

func NewSessionStore(secrets []string) *sessions.CookieStore {
	secret_bytes := [][]byte{}
	for _, b := range secrets {
		secret_bytes = append(secret_bytes, []byte(b))
	}
	store := sessions.NewCookieStore(secret_bytes...)
	store.MaxAge(60 * 60 * 24)
	store.Options.Secure = true
	store.Options.HttpOnly = true
	return store
}

func ClearSession(store *sessions.CookieStore, r *http.Request, w http.ResponseWriter) error {
	session, err := store.Get(r, sessionKey)
	if err != nil {
		return err
	}
	session.Options.MaxAge = -1
	err = session.Save(r, w)
	if err != nil {
		return err
	}
	return nil
}

func GetSession(store *sessions.CookieStore, r *http.Request) (*sessions.Session, error) {
	return store.Get(r, sessionKey)
}

func IsAuthenticated(store *sessions.CookieStore, r *http.Request) bool {
	session, err := GetSession(store, r)
	if err != nil {
		log.Println("Error getting session:", err)
		return false
	}
	return session.Values["sub"] != nil
}
