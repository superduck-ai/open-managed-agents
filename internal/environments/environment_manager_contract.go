package environments

// environmentManagerCommand 保存一次 environment-manager 后台启动所需的 stdin payload 与 shell 命令。
type environmentManagerCommand struct {
	Payload      []byte
	ShellCommand string
}

// AuthConfig 描述 environment-manager v0 启动合同中的一项鉴权配置。
// Type 决定 token 的消费方与文件描述符；session_ingress 使用签名 JWT，
// anthropic_oauth 使用本地 OAuth-compatible token，二者不能合并语义。
type AuthConfig struct {
	Type  string `json:"type"`
	Token string `json:"token"`
}
