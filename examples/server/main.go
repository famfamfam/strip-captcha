// Example: captcha server and a demo page with the React component.
//
//	npm install && npm run build
//	go run ./examples/server
//	open http://localhost:8080
package main

import (
	"context"
	_ "embed"
	"encoding/json"
	"errors"
	"flag"
	"log"
	"net/http"

	stripcaptcha "github.com/famfamfam/strip-captcha"
)

//go:embed index.html
var indexHTML []byte

func main() {
	addr := flag.String("addr", ":8080", "listen address")
	dist := flag.String("dist", "dist", "built React package (npm run build)")
	flag.Parse()

	captcha, err := stripcaptcha.New(stripcaptcha.NewMemoryStore(), stripcaptcha.Options{})
	if err != nil {
		log.Fatal(err)
	}

	mux := http.NewServeMux()
	mux.Handle("GET /api/captcha", captcha.Handler(nil))
	mux.HandleFunc("POST /api/register", func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			CaptchaID     string `json:"captcha_id"`
			CaptchaAnswer string `json:"captcha_answer"`
		}
		if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 4096)).Decode(&req); err != nil {
			http.Error(w, `{"error":"bad_request"}`, http.StatusBadRequest)
			return
		}
		err := captcha.Verify(context.Background(), stripcaptcha.RemoteIP(r), req.CaptchaID, req.CaptchaAnswer)
		w.Header().Set("Content-Type", "application/json")
		switch {
		case errors.Is(err, stripcaptcha.ErrInvalid):
			w.WriteHeader(http.StatusBadRequest)
			w.Write([]byte(`{"error":"captcha_invalid"}`))
		case err != nil:
			log.Printf("verify: %v", err)
			w.WriteHeader(http.StatusInternalServerError)
			w.Write([]byte(`{"error":"internal"}`))
		default:
			w.Write([]byte(`{"ok":true}`))
		}
	})
	mux.Handle("GET /dist/", http.StripPrefix("/dist/", http.FileServer(http.Dir(*dist))))
	mux.HandleFunc("GET /styles.css", func(w http.ResponseWriter, r *http.Request) {
		http.ServeFile(w, r, "styles.css")
	})
	mux.HandleFunc("GET /{$}", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.Write(indexHTML)
	})

	log.Printf("listening on %s", *addr)
	log.Fatal(http.ListenAndServe(*addr, mux))
}
