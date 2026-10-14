package aliyunkms

import (
	"sync"

	"github.com/aliyun/credentials-go/credentials"
)

type synchronizedCredential struct {
	credentials.Credential
	mu sync.Mutex
}

func (c *synchronizedCredential) GetCredential() (*credentials.CredentialModel, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.Credential.GetCredential()
}
