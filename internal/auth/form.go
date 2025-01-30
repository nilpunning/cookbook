package auth

import (
	"cookbook/internal/core"
	crand "crypto/rand"
	"encoding/hex"
	"fmt"
	"html/template"
	"log/slog"
	"math/rand"
	"net/http"
	"strings"
	"time"

	"github.com/gorilla/csrf"
	"golang.org/x/crypto/bcrypt"
)

func HashPassword(password []byte) string {
	salt := make([]byte, 16)
	if _, err := crand.Read(salt); err != nil {
		panic(err)
	}
	hash, err := bcrypt.GenerateFromPassword(append(password, salt...), bcrypt.DefaultCost)
	if err != nil {
		panic(err)
	}
	return fmt.Sprintf("bcrypt$%x$%x", salt, hash)
}

func ComparePasswordHash(hashedPassword string, password string) bool {
	if hashedPassword == "" {
		return false
	}

	parts := strings.Split(hashedPassword, "$")
	if len(parts) != 3 {
		slog.Error("invalid password hash in config")
		return false
	}

	if parts[0] != "bcrypt" {
		slog.Error("unsupported hash type")
		return false
	}

	salt, err := hex.DecodeString(parts[1])
	if err != nil {
		slog.Error("problem decoding salt hash", "error", err.Error())
		return false
	}

	hash, err := hex.DecodeString(parts[2])
	if err != nil {
		slog.Error("problem decoding password hash", "error", err.Error())
		return false
	}

	err = bcrypt.CompareHashAndPassword(hash, append([]byte(password), salt...))
	return err == nil
}

func AddFormBasedAuth(serveMux *http.ServeMux, state core.State, mountPoint string) (string, string) {
	loginTemplate := template.Must(template.ParseFiles(
		"templates/base.html",
		"templates/login.html",
	))

	loginURL := mountPoint + "/"
	serveMux.HandleFunc(loginURL, func(w http.ResponseWriter, r *http.Request) {
		if r.Method == "GET" {
			data := struct {
				ShowAuth  bool
				Title     string
				CsrfField template.HTML
				Username  string
				Error     string
			}{
				ShowAuth:  false,
				Title:     "Login",
				CsrfField: csrf.TemplateField(r),
			}
			if err := loginTemplate.Execute(w, data); err != nil {
				slog.Error(err.Error())
			}
			return
		}

		time.Sleep(time.Duration(1+rand.Intn(3)) * time.Second)

		err := r.ParseForm()
		if err != nil {
			slog.Error(err.Error())
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}

		username := r.Form.Get("username")
		password := r.Form.Get("password")

		invalid := func() {
			data := struct {
				ShowAuth  bool
				Title     string
				CsrfField template.HTML
				Username  string
				Error     string
			}{
				ShowAuth:  false,
				Title:     "Login",
				CsrfField: csrf.TemplateField(r),
				Username:  username,
				Error:     "Invalid username or password",
			}
			if err := loginTemplate.Execute(w, data); err != nil {
				slog.Error(err.Error())
				return
			}
		}

		if username == "" || password == "" {
			invalid()
			return
		}

		users := *state.Config.FormBasedAuthUsers
		if ComparePasswordHash(users[username], password) {
			session, err := GetSession(state.SessionStore, r)
			if err != nil {
				if err.Error() == "securecookie: the value is not valid" {
					ClearSession(state.SessionStore, r, nil)
				} else {
					slog.Error(err.Error())
					http.Error(w, err.Error(), http.StatusInternalServerError)
					return
				}
			}
			session.Values["sub"] = username
			err = session.Save(r, w)
			if err != nil {
				slog.Error(err.Error())
				http.Error(w, err.Error(), http.StatusInternalServerError)
				return
			}
			http.Redirect(w, r, "/", http.StatusSeeOther)
			return
		}

		invalid()
	})

	logoutURL := mountPoint + "/logout"
	serveMux.HandleFunc(logoutURL, func(w http.ResponseWriter, r *http.Request) {
		err := ClearSession(state.SessionStore, r, w)
		if err != nil {
			slog.Error(err.Error())
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		http.Redirect(w, r, "/", http.StatusSeeOther)
	})

	return loginURL, logoutURL
}
