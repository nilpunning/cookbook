package auth

import (
	"encoding/base64"
	"hallertau/internal/core"
	"html/template"
	"log/slog"
	"math/rand"
	"net/http"
	"time"

	"github.com/gorilla/csrf"
)

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
		password_bytes, err := base64.RawStdEncoding.DecodeString(users[username])

		if err != nil {
			slog.Error(err.Error())
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}

		if string(password_bytes) == password {
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
