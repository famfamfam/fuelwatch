package panelapi

import (
	"net/http"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"golang.org/x/crypto/bcrypt"

	"fuelwatch/internal/httpx"
	"fuelwatch/internal/store"
)

const (
	minPassword = 8
	maxLogin    = 64
)

type userRow struct {
	ID          int64      `json:"id"`
	Login       string     `json:"login"`
	CreatedAt   time.Time  `json:"created_at"`
	LastLoginAt *time.Time `json:"last_login_at"`
	Disabled    bool       `json:"disabled"`
}

func (a *API) listUsers(w http.ResponseWriter, r *http.Request) {
	rows, err := a.DB.Query(r.Context(), `SELECT id, login, created_at, last_login_at, disabled_at IS NOT NULL
		FROM users ORDER BY login`)
	if err != nil {
		httpx.Internal(w, r, err)
		return
	}
	defer rows.Close()
	items := []userRow{}
	for rows.Next() {
		var u userRow
		if err := rows.Scan(&u.ID, &u.Login, &u.CreatedAt, &u.LastLoginAt, &u.Disabled); err != nil {
			httpx.Internal(w, r, err)
			return
		}
		items = append(items, u)
	}
	httpx.OK(w, map[string]any{"items": items})
}

func checkPassword(p string) string {
	if utf8.RuneCountInString(p) < minPassword {
		return "не короче " + strconv.Itoa(minPassword) + " символов"
	}
	return ""
}

func (a *API) createUser(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Login    string `json:"login"`
		Password string `json:"password"`
	}
	if !httpx.Decode(w, r, &req, maxJSONBody) {
		return
	}
	login := strings.TrimSpace(req.Login)
	errs := map[string]string{}
	if login == "" || utf8.RuneCountInString(login) > maxLogin || strings.ContainsAny(login, " \t") {
		errs["login"] = "от 1 до 64 символов, без пробелов"
	}
	if e := checkPassword(req.Password); e != "" {
		errs["password"] = e
	}
	if len(errs) > 0 {
		httpx.FieldErrors(w, errs)
		return
	}
	hash, err := bcrypt.GenerateFromPassword([]byte(req.Password), bcrypt.DefaultCost)
	if err != nil {
		httpx.Internal(w, r, err)
		return
	}
	var u userRow
	err = a.DB.QueryRow(r.Context(), `INSERT INTO users (login, password_hash) VALUES ($1, $2)
		RETURNING id, login, created_at, last_login_at, false`, login, string(hash)).
		Scan(&u.ID, &u.Login, &u.CreatedAt, &u.LastLoginAt, &u.Disabled)
	if pe, ok := err.(*pgconn.PgError); ok && pe.Code == "23505" {
		httpx.FieldErrors(w, map[string]string{"login": "такой логин уже есть"})
		return
	}
	if err != nil {
		httpx.Internal(w, r, err)
		return
	}
	httpx.JSON(w, http.StatusCreated, u)
}

// PATCH /users/{id} {password?, disabled?}. Себя отключить нельзя. Отключение и смена пароля завершают сессии
// пользователя (кроме текущей, если меняешь свой пароль).
func (a *API) patchUser(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	if err != nil {
		httpx.Error(w, http.StatusBadRequest, "bad_request", "bad id")
		return
	}
	var req struct {
		Password *string `json:"password"`
		Disabled *bool   `json:"disabled"`
	}
	if !httpx.Decode(w, r, &req, maxJSONBody) {
		return
	}
	me := userFrom(r)
	if req.Disabled != nil && *req.Disabled && id == me.ID {
		httpx.FieldErrors(w, map[string]string{"disabled": "себя отключить нельзя"})
		return
	}
	if req.Password != nil {
		if e := checkPassword(*req.Password); e != "" {
			httpx.FieldErrors(w, map[string]string{"password": e})
			return
		}
	}
	ctx := r.Context()
	var exists bool
	if err := a.DB.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM users WHERE id=$1)`, id).Scan(&exists); err != nil {
		httpx.Internal(w, r, err)
		return
	}
	if !exists {
		httpx.Error(w, http.StatusNotFound, "not_found", "user not found")
		return
	}
	keep := ""
	if c, err := r.Cookie(cookieName); err == nil {
		keep = store.HashToken(c.Value)
	}
	if req.Password != nil {
		hash, err := bcrypt.GenerateFromPassword([]byte(*req.Password), bcrypt.DefaultCost)
		if err != nil {
			httpx.Internal(w, r, err)
			return
		}
		if _, err := a.DB.Exec(ctx, `UPDATE users SET password_hash=$2 WHERE id=$1`, id, string(hash)); err != nil {
			httpx.Internal(w, r, err)
			return
		}
		if _, err := a.DB.Exec(ctx, `DELETE FROM sessions WHERE user_id=$1 AND id <> $2`, id, keep); err != nil {
			httpx.Internal(w, r, err)
			return
		}
	}
	if req.Disabled != nil {
		if *req.Disabled {
			_, err = a.DB.Exec(ctx, `UPDATE users SET disabled_at=COALESCE(disabled_at, now()) WHERE id=$1`, id)
			if err == nil {
				_, err = a.DB.Exec(ctx, `DELETE FROM sessions WHERE user_id=$1`, id)
			}
		} else {
			_, err = a.DB.Exec(ctx, `UPDATE users SET disabled_at=NULL WHERE id=$1`, id)
		}
		if err != nil {
			httpx.Internal(w, r, err)
			return
		}
	}
	httpx.OK(w, map[string]bool{"ok": true})
}
