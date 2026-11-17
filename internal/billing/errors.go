package billing

import "errors"

var (
	errResponseNotTerminal = errors.New("响应未处于终态")
	errResponseMissingID   = errors.New("响应缺少 ID")
)

var (
	errCostProtocolInvalid    = errors.New("响应费用协议无效")
	errCostLocationMissing    = errors.New("响应费用协议缺少提取位置")
	errCostPointerInvalid     = errors.New("响应费用金额路径无效")
	errCostEventInvalid       = errors.New("响应费用事件筛选无效")
	errCostConfigurationMixed = errors.New("响应费用新旧提取配置不可混用")
)
