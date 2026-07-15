package environments

// environmentManagerCommand 保存一次 environment-manager 启动所需的 stdin payload 与 shell 命令。
type environmentManagerCommand struct {
	StdinPath    string
	Payload      []byte
	ShellCommand string
}

// AuthConfig 描述 environment-manager v0 启动合同中的一项鉴权配置。
// Type 决定 token 的消费方与文件描述符；当前 CCRv2 payload 为两个用途填入同一个
// code-session external ID，但仍保留独立配置项，避免把 ingress/relay 与模型代理鉴权混为一体。
type AuthConfig struct {
	Type  string `json:"type"`
	Token string `json:"token"`
}
