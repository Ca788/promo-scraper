package envelope

import (
	"encoding/json"
	"net/http"
	"time"

	"promo-scraper/internal/shared/pagination"
)

type Collection[T any] struct {
	Data   []T               `json:"data"`
	Meta   CollectionMeta    `json:"meta"`
	Errors map[string]string `json:"errors,omitempty"`
}

type CollectionMeta struct {
	Pagination pagination.Meta `json:"pagination"`
	ColetadoEm time.Time       `json:"coletado_em"`
}

func NewCollection[T any](items []T, meta pagination.Meta, errs map[string]string) Collection[T] {
	if items == nil {
		items = []T{}
	}
	return Collection[T]{
		Data:   items,
		Meta:   CollectionMeta{Pagination: meta, ColetadoEm: time.Now().UTC()},
		Errors: errs,
	}
}

type Error struct {
	Error ErrorDetail `json:"error"`
}

type ErrorDetail struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

func WriteJSON(w http.ResponseWriter, status int, body any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(body)
}

func WriteError(w http.ResponseWriter, status int, code, message string) {
	WriteJSON(w, status, Error{Error: ErrorDetail{Code: code, Message: message}})
}
