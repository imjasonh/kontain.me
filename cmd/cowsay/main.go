package main

import (
	"context"
	"fmt"
	"log"
	"net/http"
	"os"
	"strings"

	"github.com/imjasonh/kontain.me/pkg/serve"
)

func main() {
	ctx := context.Background()
	st, err := serve.NewStorage(ctx)
	if err != nil {
		log.Fatalf("serve.NewStorage: %v", err)
	}
	http.Handle("/v2/", &server{
		info:    log.New(os.Stdout, "I ", log.Ldate|log.Ltime|log.Lshortfile),
		error:   log.New(os.Stderr, "E ", log.Ldate|log.Ltime|log.Lshortfile),
		storage: st,
	})
	http.Handle("/", http.RedirectHandler("https://github.com/imjasonh/kontain.me/blob/main/cmd/cowsay", http.StatusSeeOther))

	log.Println("Starting...")
	port := os.Getenv("PORT")
	if port == "" {
		port = "8080"
		log.Printf("Defaulting to port %s", port)
	}
	log.Printf("Listening on port %s", port)
	log.Fatal(http.ListenAndServe(fmt.Sprintf(":%s", port), nil))
}

type server struct {
	info, error *log.Logger
	storage     *serve.Storage
}

func (s *server) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	s.info.Println("handler:", r.Method, r.URL)
	path := strings.TrimPrefix(r.URL.String(), "/v2/")

	switch {
	case path == "":
		// API Version check.
		w.Header().Set("Docker-Distribution-API-Version", "registry/2.0")
		return
	case strings.Contains(path, "/manifests/"):
		s.serveCowsayManifest(w, r)
	default:
		serve.Error(w, serve.ErrNotFound)
	}
}

func (s *server) serveCowsayManifest(w http.ResponseWriter, r *http.Request) {
	path := strings.TrimPrefix(r.URL.Path, "/v2/")
	parts := strings.Split(path, "/")
	msg := strings.Join(parts[:len(parts)-2], " ")

	serve.Error(w, fmt.Errorf(`
  %s
< %s >
  %s
          \
            \
              \
                \   ▄æΦΦΦ¥╗▄
                 ,▀╨ ...... ╨▄
                ▄╙╙▀╙.........╨p
               ╫ ..............╙µ
               ▀...█▓.......▄█▌.╫
              j▒.. ██.......╨██.║
               ▀,...............╜
          ╓e⌐7░ ▓^."w...^..zL."╫ j7Tw▄
         ╙▄,..▄╜ ...... ─ ..... ╨▄..,▄▀
             ▌ .... ╣......▌ .... ▓
             ▀╪▄▄╧╙  ▌....▄ └╙╧▄▄╝▀
                      ▀¥╨▀
`, strings.Repeat("-", len(msg)), msg, strings.Repeat("-", len(msg))))
}
