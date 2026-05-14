package serve

import (
	"encoding/json"
	"errors"
	"net/http"

	"github.com/google/go-containerregistry/pkg/v1/remote/transport"
)

var ErrNotFound = errors.New("not found")

func Error(w http.ResponseWriter, err error) {
	code := "MANIFEST_UNKNOWN"
	httpCode := http.StatusNotFound
	if terr, ok := err.(*transport.Error); ok {
		http.Error(w, "", terr.StatusCode)
		json.NewEncoder(w).Encode(terr.Errors)
		return
	}

	http.Error(w, "", httpCode)
	json.NewEncoder(w).Encode(&resp{
		Errors: []e{{
			Code:    code,
			Message: err.Error(),
		}},
	})
}

type resp struct {
	Errors []e `json:"errors"`
}

type e struct {
	Code    string `json:"code"`
	Message string `json:"message"`
	Reason  string `json:"reason,omitempty"`
}
