package vaults

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

type OAuthExpiresIn int64

const maxOAuthExpiresInSeconds = int64((1<<63 - 1) / int64(time.Second))

func (e *OAuthExpiresIn) UnmarshalJSON(data []byte) error {
	data = bytes.TrimSpace(data)
	if len(data) == 0 || string(data) == "null" {
		*e = 0
		return nil
	}
	if data[0] == '"' {
		var raw string
		if err := json.Unmarshal(data, &raw); err != nil {
			return err
		}
		raw = strings.TrimSpace(raw)
		if raw == "" {
			*e = 0
			return nil
		}
		parsed, err := strconv.ParseInt(raw, 10, 64)
		if err != nil {
			if errors.Is(err, strconv.ErrRange) {
				return errors.New("token endpoint expires_in exceeds supported duration")
			}
			*e = 0
			return nil
		}
		return e.setSeconds(parsed)
	}
	var asFloat float64
	if err := json.Unmarshal(data, &asFloat); err != nil {
		*e = 0
		return nil
	}
	if asFloat > float64(maxOAuthExpiresInSeconds) {
		return errors.New("token endpoint expires_in exceeds supported duration")
	}
	return e.setSeconds(int64(asFloat))
}
func (e *OAuthExpiresIn) setSeconds(value int64) error {
	if value > maxOAuthExpiresInSeconds {
		return errors.New("token endpoint expires_in exceeds supported duration")
	}
	*e = OAuthExpiresIn(value)
	return nil
}

// OAuthTokenEndpointExchange is the grant-agnostic token-endpoint POST seam.
// Callers set Form with grant_type and grant-specific fields; client_secret is
// applied here when TokenEndpointAuthMethod is client_secret_post.
type OAuthTokenEndpointExchange struct {
	TokenEndpoint           string
	ClientID                string
	ClientSecret            string
	TokenEndpointAuthMethod string
	Form                    url.Values
}

// OAuthTokenEndpointResult is the successful token-endpoint response surface.
type OAuthTokenEndpointResult struct {
	AccessToken  string
	RefreshToken string
	Scope        string
	ExpiresIn    OAuthExpiresIn
}

type oauthTokenEndpointWire struct {
	TokenType    json.RawMessage `json:"token_type"`
	AccessToken  string          `json:"access_token"`
	RefreshToken string          `json:"refresh_token"`
	ExpiresIn    OAuthExpiresIn  `json:"expires_in"`
	Scope        string          `json:"scope"`
}

// ExchangeOAuthTokenEndpoint POSTs to an OAuth token endpoint with the shared
// client-auth and response-decoding rules used by refresh and code exchange.
func ExchangeOAuthTokenEndpoint(ctx context.Context, client *http.Client, in OAuthTokenEndpointExchange) (OAuthTokenEndpointResult, error) {
	method := in.TokenEndpointAuthMethod
	clientSecret := in.ClientSecret
	form := cloneURLValues(in.Form)
	switch method {
	case "client_secret_basic":
		if clientSecret == "" {
			return OAuthTokenEndpointResult{}, tokenEndpointAuthMissingSecret("client_secret_basic")
		}
	case "client_secret_post":
		if clientSecret == "" {
			return OAuthTokenEndpointResult{}, tokenEndpointAuthMissingSecret("client_secret_post")
		}
		form.Set("client_secret", clientSecret)
	case "", "none":
		method = "none"
	default:
		return OAuthTokenEndpointResult{}, unsupportedTokenAuthMethod(method)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, in.TokenEndpoint, strings.NewReader(form.Encode()))
	if err != nil {
		return OAuthTokenEndpointResult{}, err
	}
	req.Header.Set("Accept", "application/json")
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	if method == "client_secret_basic" {
		basic := url.QueryEscape(in.ClientID) + ":" + url.QueryEscape(clientSecret)
		req.Header.Set("Authorization", "Basic "+base64.StdEncoding.EncodeToString([]byte(basic)))
	}
	if client == nil {
		client = defaultOAuthHTTPClient()
	}
	resp, err := client.Do(req)
	if err != nil {
		return OAuthTokenEndpointResult{}, err
	}
	defer resp.Body.Close()
	respBody, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return OAuthTokenEndpointResult{}, err
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return OAuthTokenEndpointResult{}, tokenEndpointStatus(resp.StatusCode)
	}
	var wire oauthTokenEndpointWire
	if json.Unmarshal(respBody, &wire) != nil {
		return OAuthTokenEndpointResult{}, errors.New("token endpoint returned invalid token response")
	}
	if len(wire.TokenType) != 0 {
		var tokenType string
		if json.Unmarshal(wire.TokenType, &tokenType) != nil || !strings.EqualFold(tokenType, "Bearer") {
			return OAuthTokenEndpointResult{}, errors.New("token endpoint returned unsupported token_type")
		}
	}
	accessToken := strings.TrimSpace(wire.AccessToken)
	if accessToken == "" {
		return OAuthTokenEndpointResult{}, tokenEndpointMissingAccessToken()
	}
	return OAuthTokenEndpointResult{
		AccessToken:  accessToken,
		RefreshToken: strings.TrimSpace(wire.RefreshToken),
		Scope:        strings.TrimSpace(wire.Scope),
		ExpiresIn:    wire.ExpiresIn,
	}, nil
}

func cloneURLValues(values url.Values) url.Values {
	if values == nil {
		return url.Values{}
	}
	out := make(url.Values, len(values))
	for key, list := range values {
		out[key] = append([]string(nil), list...)
	}
	return out
}
