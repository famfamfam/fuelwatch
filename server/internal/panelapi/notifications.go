package panelapi

import (
	"net/http"
	"strconv"

	"github.com/go-chi/chi/v5"

	"fuelwatch/internal/httpx"
	"fuelwatch/internal/notify"
)

// GET /notifications?device=&category=&severity=&unread=&cursor=&limit= — от новых к старым, cursor = id.
func (a *API) listNotifications(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	limit := pageSize(r)
	var cursor int64
	if c := q.Get("cursor"); c != "" {
		n, err := strconv.ParseInt(c, 10, 64)
		if err != nil {
			httpx.Error(w, http.StatusBadRequest, "bad_request", "bad cursor")
			return
		}
		cursor = n
	}
	var types []string
	if cat := q.Get("category"); cat != "" {
		types = notify.Categories[cat]
		if types == nil {
			httpx.Error(w, http.StatusBadRequest, "bad_request", "unknown category")
			return
		}
	}
	rows, err := a.DB.Query(r.Context(), `SELECT `+notify.Cols+` FROM notifications
		WHERE ($1 = '' OR device_id = $1)
		  AND ($2 = '' OR severity = $2)
		  AND (NOT $3 OR read_at IS NULL)
		  AND ($4::text[] IS NULL OR type = ANY($4))
		  AND ($5 = 0 OR id < $5)
		ORDER BY id DESC LIMIT $6`,
		q.Get("device"), q.Get("severity"), q.Get("unread") == "true", types, cursor, limit+1)
	if err != nil {
		httpx.Internal(w, r, err)
		return
	}
	defer rows.Close()
	items := []notify.Notification{}
	for rows.Next() {
		n, err := notify.Scan(rows)
		if err != nil {
			httpx.Internal(w, r, err)
			return
		}
		items = append(items, n)
	}
	if err := rows.Err(); err != nil {
		httpx.Internal(w, r, err)
		return
	}
	var next *string
	if len(items) > limit {
		items = items[:limit]
		c := strconv.FormatInt(items[len(items)-1].ID, 10)
		next = &c
	}
	httpx.OK(w, map[string]any{"items": items, "next_cursor": next})
}

func (a *API) unreadCount(w http.ResponseWriter, r *http.Request) {
	var n int
	if err := a.DB.QueryRow(r.Context(), `SELECT count(*) FROM notifications WHERE read_at IS NULL`).Scan(&n); err != nil {
		httpx.Internal(w, r, err)
		return
	}
	httpx.OK(w, map[string]int{"count": n})
}

func (a *API) readOne(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	if err != nil {
		httpx.Error(w, http.StatusBadRequest, "bad_request", "bad id")
		return
	}
	if _, err := a.DB.Exec(r.Context(), `UPDATE notifications SET read_at=now(), read_by=$2 WHERE id=$1 AND read_at IS NULL`,
		id, userFrom(r).ID); err != nil {
		httpx.Internal(w, r, err)
		return
	}
	httpx.OK(w, map[string]bool{"ok": true})
}

func (a *API) readAll(w http.ResponseWriter, r *http.Request) {
	if _, err := a.DB.Exec(r.Context(), `UPDATE notifications SET read_at=now(), read_by=$1 WHERE read_at IS NULL`,
		userFrom(r).ID); err != nil {
		httpx.Internal(w, r, err)
		return
	}
	httpx.OK(w, map[string]bool{"ok": true})
}
