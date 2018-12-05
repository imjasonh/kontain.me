package main

import (
	"fmt"
	"io"
	"log"
	"net/http"
)

const appJunk = "https://app-j33hbo5ywa-uc.a.run.app"

func main() {
	http.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		req, err := http.NewRequest(r.Method, fmt.Sprintf("%s/%s", appJunk, r.URL.Path), nil)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}

		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		for k, v := range resp.Header {
			w.Header().Set(k, v[0])
		}
		w.WriteHeader(resp.StatusCode)
		io.Copy(w, resp.Body)
	})
	log.Fatal(http.ListenAndServe(":8080", nil))
}
