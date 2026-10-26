package billing

type Usage struct {
	InputTokens      *int64       `json:"input_tokens,omitzero"`
	OutputTokens     *int64       `json:"output_tokens,omitzero"`
	CacheReadTokens  *int64       `json:"cache_read_input_tokens,omitzero"`
	CacheWriteTokens *int64       `json:"cache_creation_input_tokens,omitzero"`
	CacheCreation    *CacheUsage  `json:"cache_creation,omitzero"`
	ServerToolUse    *SearchUsage `json:"server_tool_use,omitzero"`
}

type CacheUsage struct {
	FiveMinutes *int64 `json:"ephemeral_5m_input_tokens,omitzero"`
	OneHour     *int64 `json:"ephemeral_1h_input_tokens,omitzero"`
}

type SearchUsage struct {
	Requests *int64 `json:"web_search_requests,omitzero"`
}

func UsageCount(value *int64) int64 {
	if value == nil {
		return 0
	}
	return max(*value, 0)
}

func (u Usage) Tokens() TokenUsage {
	write := UsageCount(u.CacheWriteTokens)
	if u.CacheWriteTokens == nil && u.CacheCreation != nil {
		write = UsageCount(u.CacheCreation.FiveMinutes) + UsageCount(u.CacheCreation.OneHour)
	}
	tokens := TokenUsage{InputTokens: UsageCount(u.InputTokens), OutputTokens: UsageCount(u.OutputTokens), CacheReadInputTokens: UsageCount(u.CacheReadTokens), CacheCreationInputTokens: write}
	if u.CacheCreation != nil {
		tokens.CacheWrite5mTokens = UsageCount(u.CacheCreation.FiveMinutes)
		tokens.CacheWrite1hTokens = UsageCount(u.CacheCreation.OneHour)
	}
	return tokens
}

func (u Usage) Searches() int64 {
	if u.ServerToolUse == nil {
		return 0
	}
	return UsageCount(u.ServerToolUse.Requests)
}

func (u Usage) Complete() bool {
	if u.InputTokens == nil || u.OutputTokens == nil {
		return false
	}
	values := []*int64{u.InputTokens, u.OutputTokens, u.CacheReadTokens, u.CacheWriteTokens}
	if u.CacheCreation != nil {
		values = append(values, u.CacheCreation.FiveMinutes, u.CacheCreation.OneHour)
	}
	if u.ServerToolUse != nil {
		values = append(values, u.ServerToolUse.Requests)
	}
	for _, value := range values {
		if value != nil && *value < 0 {
			return false
		}
	}
	return true
}
