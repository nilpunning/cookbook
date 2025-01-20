package auth

import (
	"encoding/base64"
	"log/slog"
	"net/http"

	"github.com/gorilla/sessions"
)

var sessionKey = "session"

func NewSessionStore(secrets []string, secureCookies bool) *sessions.CookieStore {
	secret_bytes := [][]byte{}
	for _, s := range secrets {
		d, err := base64.RawStdEncoding.DecodeString(s)
		if err != nil {
			panic(err)
		}
		secret_bytes = append(secret_bytes, d)
	}
	store := sessions.NewCookieStore(secret_bytes...)
	store.MaxAge(60 * 60 * 24)
	store.Options.Secure = secureCookies
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
		if err.Error() == "securecookie: the value is not valid" {
			ClearSession(store, r, nil)
			return false
		} else {
			slog.Error(err.Error())
			return false
		}
	}
	return session.Values["sub"] != nil
}
