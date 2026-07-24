package websearch

import "time"

type SearchRequest struct {
	Query   string
	Options SearchOptions
}

type SearchOptions struct {
	MaxResults       int
	IncludeDomains   []string
	ExcludeDomains   []string
	SearchMode       string
	Country          string
	SearchLanguage   string
	UILanguage       string
	Freshness        string
	StartPublishedAt time.Time
	EndPublishedAt   time.Time
	SafeSearch       string
	Spellcheck       *bool
	ResultFilter     string
	Goggles          []string
	ExtraSnippets    bool
	Units            string
	PageToken        string
	Content          ContentOptions
}

type ContentOptions struct {
	Text          bool
	Highlights    bool
	Summary       bool
	MaxCharacters int
}

type SearchResponse struct {
	Results       []Result
	HasMore       bool
	NextPageToken string
	RequestID     string
}

type Result struct {
	ID            string
	Title         string
	URL           string
	Snippet       string
	Text          string
	Highlights    []string
	Summary       string
	Author        string
	Favicon       string
	PublishedDate string
	ExtraSnippets []string
	PageAge       string
}
