package serve

import (
	"encoding/json"
	"errors"
	"net/http"

	"github.com/google/go-containerregistry/pkg/v1/remote/transport"
)

var (
	ErrNotFound = errors.New("repository or commit not found")
	ErrInvalid  = errors.New("requested manifest is invalid")
)

func Error(w http.ResponseWriter, err error) {
	code := "INTERNAL_ERROR"
	httpCode := http.StatusNotFound
	if terr, ok := err.(*transport.Error); ok {
		http.Error(w, "", terr.StatusCode)
		json.NewEncoder(w).Encode(terr.Errors)
		return
	}

	if err == ErrNotFound {
		code = "MANIFEST_UNKNOWN"
	} else if err == ErrInvalid {
		code = "NAME_INVALID"
		httpCode = http.StatusBadRequest
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
