package billing

import "errors"

var (
	errResponseNotTerminal = errors.New("响应未处于终态")
	errResponseMissingID   = errors.New("响应缺少 ID")
)
