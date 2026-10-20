package webtools

import "context"

type SearchProvider interface {
	Search(context.Context, SearchRequest) (SearchResponse, error)
}

type SearchRequest struct {
	Query string `json:"query"`
}

type Source struct {
	Title   string  `json:"title"`
	URL     string  `json:"url"`
	Snippet string  `json:"snippet"`
	Date    *string `json:"date,omitempty"`
}

type SearchResponse struct {
	Sources []Source `json:"sources"`
}

type FetchRequest struct {
	URL string `json:"url"`
}

type FetchResponse struct {
	URL        string `json:"url"`
	StatusCode int    `json:"status_code"`
	Title      string `json:"title"`
	Content    string `json:"content"`
	Truncated  bool   `json:"truncated"`
}
