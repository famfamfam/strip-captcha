package stripcaptcha

import (
	"encoding/json"
	"errors"
	"log"
	"net"
	"net/http"
	"strconv"
)

// Handler отдаёт новую капчу JSON-ом в формате, который ждёт React-компонент.
// clientKey достаёт из запроса ключ клиента (обычно IP) для лимита выдач;
// nil — RemoteIP. За прокси передайте функцию, читающую доверенный заголовок:
// X-Forwarded-For без проверки подделывается клиентом и обходит лимит.
func (s *Service) Handler(clientKey func(*http.Request) string) http.Handler {
	if clientKey == nil {
		clientKey = RemoteIP
	}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			w.Header().Set("Allow", http.MethodGet)
			writeJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "method_not_allowed"})
			return
		}
		ch, err := s.Generate(r.Context(), clientKey(r))
		if err != nil {
			if errors.Is(err, ErrRateLimited) {
				w.Header().Set("Retry-After", strconv.Itoa(int(s.opt.RateWindow.Seconds())))
				writeJSON(w, http.StatusTooManyRequests, map[string]string{"error": "rate_limited"})
				return
			}
			log.Printf("stripcaptcha: %v", err)
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "internal"})
			return
		}
		writeJSON(w, http.StatusOK, ch)
	})
}

// RemoteIP — IP из r.RemoteAddr, без порта.
func RemoteIP(r *http.Request) string {
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return r.RemoteAddr
	}
	return host
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	// Каждая капча одноразовая — кешировать её нельзя ни браузеру, ни прокси
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}
