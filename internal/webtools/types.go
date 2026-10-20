package webtools

type SearchRequest struct {
	Query            string  `json:"query"`
	Count            *int    `json:"count,omitempty"`
	OriginalQuestion *string `json:"ori_question,omitempty"`
	Localization     string  `json:"localization,omitempty"`
	NeedContent      bool    `json:"need_content"`
}

type SearchResult struct {
	RerankScore float64 `json:"rerank_score"`
	Title       string  `json:"title"`
	URL         string  `json:"url"`
	Snippet     string  `json:"snippet"`
	Summary     string  `json:"summary"`
	Content     string  `json:"content"`
	SiteName    *string `json:"site_name"`
	SiteIcon    *string `json:"site_icon"`
	Date        *string `json:"date"`
	Authority   int     `json:"authority"`
}

type SearchResponse struct {
	Results []SearchResult `json:"results"`
}

type FetchRequest struct {
	URL string `json:"url"`
}

type FetchResponse struct {
	Title   string `json:"title"`
	Content string `json:"content"`
}
