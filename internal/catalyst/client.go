// SPDX-License-Identifier: MPL-2.0

package catalyst

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"sync"
	"time"
)

// Config contains credentials and regional routing for a single provider instance.
type Config struct {
	DataCenter, Environment, OrganizationID                        string
	AccessToken, ClientID, ClientSecret, RefreshToken, RedirectURI string
}

type endpoints struct{ api, accounts string }

// Regional Catalyst API hosts and canonical OAuth accounts hosts.
// Sources: https://docs.catalyst.zoho.com/en/api/introduction/multi-dc-support/
// and https://accounts.zoho.com/oauth/serverinfo (OAuth hostnames).
var regions = map[string]endpoints{
	"us": {"https://api.catalyst.zoho.com", "https://accounts.zoho.com"},
	"eu": {"https://api.catalyst.zoho.eu", "https://accounts.zoho.eu"},
	"in": {"https://api.catalyst.zoho.in", "https://accounts.zoho.in"},
	"au": {"https://api.catalyst.zoho.com.au", "https://accounts.zoho.com.au"},
	"ca": {"https://api.catalyst.zohocloud.ca", "https://accounts.zohocloud.ca"},
	"jp": {"https://api.catalyst.zoho.jp", "https://accounts.zoho.jp"},
	"sa": {"https://api.catalyst.zoho.sa", "https://accounts.zoho.sa"},
}

var numericID = regexp.MustCompile(`^[1-9][0-9]*$`)

// ValidID checks IDs without converting them to floating point numbers.
func ValidID(id string) bool { return numericID.MatchString(id) }

// Client shares an in-memory OAuth token cache across a provider's data sources.
type Client struct {
	config              Config
	apiURL, accountsURL string
	http                *http.Client
	mu                  sync.Mutex
	token               string
	expiresAt           time.Time
}

// New validates configuration without making network requests.
func New(config Config, httpClient *http.Client) (*Client, error) {
	if config.DataCenter == "" {
		config.DataCenter = "us"
	}
	if config.Environment == "" {
		config.Environment = "Development"
	}
	hosts, ok := regions[config.DataCenter]
	if !ok {
		return nil, errors.New("data_center must be one of: us, eu, in, au, ca, jp, sa")
	}
	if config.Environment != "Development" && config.Environment != "Production" {
		return nil, errors.New("environment must be Development or Production")
	}
	if config.OrganizationID != "" && !ValidID(config.OrganizationID) {
		return nil, errors.New("organization_id must be a positive integer written as a string")
	}
	hasRefresh := config.ClientID != "" || config.ClientSecret != "" || config.RefreshToken != "" || config.RedirectURI != ""
	if config.AccessToken != "" && hasRefresh {
		return nil, errors.New("use access_token OR client_id, client_secret and refresh_token; unset the unused credentials, including their CATALYST_ environment variables")
	}
	if config.AccessToken == "" && (config.ClientID == "" || config.ClientSecret == "" || config.RefreshToken == "") {
		return nil, errors.New("set CATALYST_ACCESS_TOKEN, or all of CATALYST_CLIENT_ID, CATALYST_CLIENT_SECRET and CATALYST_REFRESH_TOKEN (equivalent provider attributes are also accepted)")
	}
	for _, value := range []string{config.AccessToken, config.ClientID, config.ClientSecret, config.RefreshToken} {
		if strings.ContainsAny(value, "\r\n") {
			return nil, errors.New("credentials must not contain line breaks")
		}
	}
	if config.RedirectURI != "" {
		uri, err := url.Parse(config.RedirectURI)
		if err != nil || uri.Host == "" || (uri.Scheme != "https" && uri.Scheme != "http") {
			return nil, errors.New("redirect_uri must be an absolute HTTP or HTTPS URL")
		}
	}
	hc := http.Client{Timeout: 30 * time.Second}
	if httpClient != nil {
		hc = *httpClient
	}
	if hc.Timeout == 0 {
		hc.Timeout = 30 * time.Second
	}
	// Never forward OAuth form bodies or authorization headers through redirects.
	hc.CheckRedirect = func(_ *http.Request, _ []*http.Request) error { return http.ErrUseLastResponse }
	return &Client{config: config, apiURL: hosts.api, accountsURL: hosts.accounts, http: &hc}, nil
}

func (c *Client) accessToken(ctx context.Context) (string, error) {
	if c.config.AccessToken != "" {
		return c.config.AccessToken, nil
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if err := ctx.Err(); err != nil {
		return "", err
	}
	if c.token != "" && time.Now().Before(c.expiresAt) {
		return c.token, nil
	}
	form := url.Values{
		"grant_type": {"refresh_token"}, "client_id": {c.config.ClientID},
		"client_secret": {c.config.ClientSecret}, "refresh_token": {c.config.RefreshToken},
	}
	if c.config.RedirectURI != "" {
		form.Set("redirect_uri", c.config.RedirectURI)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.accountsURL+"/oauth/v2/token", strings.NewReader(form.Encode()))
	if err != nil {
		return "", errors.New("could not construct OAuth request")
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Accept", "application/json")
	res, err := c.http.Do(req)
	if err != nil {
		return "", requestError(ctx, "OAuth token request")
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusOK {
		return "", statusError("OAuth token request", res.StatusCode)
	}
	var token struct {
		AccessToken string `json:"access_token"`
		ExpiresIn   int64  `json:"expires_in"`
		Error       string `json:"error"`
	}
	if err := decode(res.Body, &token); err != nil {
		return "", errors.New("OAuth server returned an invalid JSON response")
	}
	if token.Error != "" {
		switch token.Error {
		case "invalid_client", "invalid_client_secret", "invalid_code", "invalid_grant":
			return "", fmt.Errorf("OAuth refresh failed (%s); check the credentials and data center", token.Error)
		default:
			return "", errors.New("OAuth refresh was rejected; check credentials, data center and token limits")
		}
	}
	if token.AccessToken == "" || strings.ContainsAny(token.AccessToken, "\r\n") || token.ExpiresIn <= 0 || token.ExpiresIn > 86400 {
		return "", errors.New("OAuth response is missing a valid access_token or expires_in")
	}
	lifetime := time.Duration(token.ExpiresIn) * time.Second
	margin := min(30*time.Second, lifetime/10)
	c.token = token.AccessToken
	c.expiresAt = time.Now().Add(lifetime - margin)
	return c.token, nil
}

func (c *Client) invalidate(token string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	// A parallel request may already have refreshed the rejected token.
	if c.token == token {
		c.expiresAt = time.Time{}
	}
}

// ID accepts either API representation without losing integer precision.
type ID string

func (id *ID) UnmarshalJSON(raw []byte) error {
	var value string
	if len(raw) > 0 && raw[0] == '"' {
		if err := json.Unmarshal(raw, &value); err != nil {
			return errors.New("invalid identifier")
		}
	} else {
		value = string(raw)
	}
	if !ValidID(value) {
		return errors.New("invalid identifier")
	}
	*id = ID(value)
	return nil
}

// Project contains only the metadata exposed by the Terraform data source.
type Project struct {
	ID            ID     `json:"id"`
	Name          string `json:"project_name"`
	Type          string `json:"project_type"`
	Timezone      string `json:"timezone"`
	DomainDetails struct {
		Name string `json:"project_domain_name"`
		URL  string `json:"project_domain"`
	} `json:"project_domain_details"`
}

// GetProject reads project metadata. It retries one unauthorized request only in refresh-token mode.
func (c *Client) GetProject(ctx context.Context, id string) (*Project, error) {
	if !ValidID(id) {
		return nil, errors.New("project id must be a positive integer written as a string")
	}
	for attempt := 0; attempt < 2; attempt++ {
		token, err := c.accessToken(ctx)
		if err != nil {
			return nil, err
		}
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.apiURL+"/baas/v1/project/"+id, nil)
		if err != nil {
			return nil, errors.New("could not construct project request")
		}
		req.Header.Set("Authorization", "Zoho-oauthtoken "+token)
		req.Header.Set("Accept", "application/json")
		req.Header.Set("Environment", c.config.Environment)
		if c.config.OrganizationID != "" {
			req.Header.Set("CATALYST-ORG", c.config.OrganizationID)
		}
		res, err := c.http.Do(req)
		if err != nil {
			return nil, requestError(ctx, "Catalyst project request")
		}
		if res.StatusCode == http.StatusUnauthorized && c.config.RefreshToken != "" && attempt == 0 {
			_ = res.Body.Close()
			c.invalidate(token)
			continue
		}
		project, parseErr := readProject(res, id)
		_ = res.Body.Close()
		return project, parseErr
	}
	return nil, errors.New("Catalyst authorization failed after refreshing the access token")
}

func readProject(res *http.Response, id string) (*Project, error) {
	if res.StatusCode != http.StatusOK {
		return nil, statusError("Catalyst project request", res.StatusCode)
	}
	var envelope struct {
		Status string   `json:"status"`
		Data   *Project `json:"data"`
	}
	if err := decode(res.Body, &envelope); err != nil {
		return nil, errors.New("Catalyst returned an invalid project response")
	}
	if envelope.Status != "success" || envelope.Data == nil {
		return nil, errors.New("Catalyst did not return a successful project response")
	}
	if string(envelope.Data.ID) != id || envelope.Data.Name == "" {
		return nil, errors.New("Catalyst returned incomplete or mismatched project metadata")
	}
	return envelope.Data, nil
}

func decode(body io.Reader, target any) error {
	const limit = 1024 * 1024
	data, err := io.ReadAll(io.LimitReader(body, limit+1))
	if err != nil {
		return err
	}
	if len(data) > limit {
		return errors.New("response exceeds size limit")
	}
	return json.Unmarshal(data, target)
}

// Raw response bodies and transport errors may contain secrets; keep diagnostics controlled.
func requestError(ctx context.Context, operation string) error {
	if ctx.Err() != nil {
		return fmt.Errorf("%s: %w", operation, ctx.Err())
	}
	return fmt.Errorf("%s failed; check connectivity, proxy settings and TLS certificates", operation)
}

func statusError(operation string, status int) error {
	hint := "unexpected response from server"
	switch status {
	case 401:
		hint = "authentication failed; check credentials and data center"
	case 403:
		hint = "access denied; check OAuth scopes and project membership"
	case 404:
		hint = "not found; check project ID, organization and data center"
	case 429:
		hint = "rate limit reached; wait before retrying"
	}
	return fmt.Errorf("%s returned HTTP %d: %s", operation, status, hint)
}
