package pagination

import (
	"net/url"
	"strconv"
)

const (
	DefaultPage  = 1
	DefaultLimit = 20
	MinLimit     = 1
	MaxLimit     = 100
)

type Params struct {
	Page  int `json:"page"`
	Limit int `json:"limit"`
}

type Meta struct {
	Page       int  `json:"page"`
	Limit      int  `json:"limit"`
	Total      int  `json:"total"`
	TotalPages int  `json:"total_pages"`
	HasNext    bool `json:"has_next"`
	HasPrev    bool `json:"has_prev"`
}

func FromQuery(q url.Values) Params {
	return Params{
		Page:  clampPage(atoiOrDefault(q.Get("page"), DefaultPage)),
		Limit: clampLimit(atoiOrDefault(q.Get("limit"), DefaultLimit)),
	}
}

func (p Params) Offset() int {
	return (p.Page - 1) * p.Limit
}

func Slice[T any](items []T, p Params) ([]T, Meta) {
	total := len(items)
	offset := p.Offset()
	end := offset + p.Limit
	if offset > total {
		offset = total
	}
	if end > total {
		end = total
	}
	out := items[offset:end]
	if out == nil {
		out = []T{}
	}
	totalPages := 0
	if p.Limit > 0 {
		totalPages = (total + p.Limit - 1) / p.Limit
	}
	return out, Meta{
		Page:       p.Page,
		Limit:      p.Limit,
		Total:      total,
		TotalPages: totalPages,
		HasNext:    p.Page < totalPages,
		HasPrev:    p.Page > 1 && total > 0,
	}
}

func atoiOrDefault(s string, def int) int {
	if s == "" {
		return def
	}
	n, err := strconv.Atoi(s)
	if err != nil {
		return def
	}
	return n
}

func clampPage(n int) int {
	if n < 1 {
		return DefaultPage
	}
	return n
}

func clampLimit(n int) int {
	if n < MinLimit {
		return DefaultLimit
	}
	if n > MaxLimit {
		return MaxLimit
	}
	return n
}
