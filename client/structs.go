package client

import (
	"encoding/json"
	"net/http"
)

type GraphAuthResponse struct {
	TokenType    string      `json:"token_type"`
	ExpiresIn    json.Number `json:"expires_in"`
	ExtExpiresIn json.Number `json:"ext_expires_in"`
	AccessToken  string      `json:"access_token"`
}

type GraphClient struct {
	TenantId     string `json:"tenant_id"`
	ClientId     string `json:"client_id"`
	ClientSecret string `json:"client_secret"`
	AccessToken  string `json:"access_token"`
	httpClient   *http.Client
}

type GraphSecurityAlertsResponse struct {
	Context  string        `json:"@odata.context"`
	NextLink string        `json:"@odata.nextLink"`
	Value    []interface{} `json:"value"`
}
