package httpx

import (
	"net"
	"net/http"
	"sync"
	"time"
)

const limiterPeriod = time.Minute

// Limiter — не больше PerMinute запросов с одного ключа (IP) за минуту. Для входа и привязки телефона:
// перебор пароля или кода привязки становится бесполезным.
type Limiter struct {
	PerMinute int

	mu   sync.Mutex
	seen map[string]*limiterWindow
}

type limiterWindow struct {
	start time.Time
	n     int
}

func (l *Limiter) Allow(key string) bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	now := time.Now()
	if l.seen == nil {
		l.seen = map[string]*limiterWindow{}
	}
	// Старые окна выбрасываются, чтобы карта не росла бесконечно.
	if len(l.seen) > 1000 {
		for k, w := range l.seen {
			if now.Sub(w.start) > limiterPeriod {
				delete(l.seen, k)
			}
		}
	}
	w := l.seen[key]
	if w == nil || now.Sub(w.start) > limiterPeriod {
		w = &limiterWindow{start: now}
		l.seen[key] = w
	}
	w.n++
	return w.n <= l.PerMinute
}

// ClientIP — адрес клиента (за nginx его подставляет middleware.RealIP).
func ClientIP(r *http.Request) string {
	if ip, _, err := net.SplitHostPort(r.RemoteAddr); err == nil {
		return ip
	}
	return r.RemoteAddr
}

// TooMany — стандартный ответ 429.
func TooMany(w http.ResponseWriter) {
	Error(w, http.StatusTooManyRequests, "rate_limited", "слишком много попыток, подождите минуту")
}
