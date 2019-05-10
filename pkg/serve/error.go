package serve

import (
	"encoding/json"
	"errors"
	"net/http"
)

var (
	ErrNotFound = errors.New("repository or commit not found")
	ErrInvalid  = errors.New("requested manifest is invalid")
)

func Error(w http.ResponseWriter, err error) {
	code := "INTERNAL_ERROR"
	httpCode := http.StatusInternalServerError

	if err == ErrNotFound {
		code = "MANIFEST_UNKNOWN"
		httpCode = http.StatusNotFound
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
