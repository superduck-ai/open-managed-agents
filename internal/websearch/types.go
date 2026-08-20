package websearch

type SearchRequest struct {
	Query   string
	Options SearchOptions
}

type SearchOptions struct {
	MaxResults     int
	IncludeDomains []string
	ExcludeDomains []string
	PageToken      string
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
