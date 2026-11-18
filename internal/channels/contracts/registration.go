package contracts

import (
	"context"
	"errors"
)

type RegistrationOptions struct {
	Name       string
	OnQRCode   func(url string, expiresIn int)
	OnInterval func(int)
}
type RegistrationResult struct{ ClientID, ClientSecret string }

var ErrRegistrationDenied = errors.New("应用授权被拒绝")
var ErrRegistrationExpired = errors.New("应用授权已过期")

type Registrar interface {
	RegisterApp(context.Context, *RegistrationOptions) (*RegistrationResult, error)
}
