// SPDX-License-Identifier: MPL-2.0
package catalyst

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"mime"
	"net/http"
	"net/url"
	"path"
	"regexp"
	"strconv"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"
)

var ErrStratusObjectNotFound = errors.New("the Stratus object is absent after parent/list verification")
var errStratusBucketNotFound = errors.New("the Stratus bucket is absent after project/list verification")
var errStratusPending = errors.New("the Stratus metadata and content have not converged")
var stratusBucketName = regexp.MustCompile(`^[a-z0-9](?:[a-z0-9-]{0,61}[a-z0-9])?$`)

func ValidStratusBucketName(name string) bool { return stratusBucketName.MatchString(name) }
func ValidStratusObjectKey(key string) bool {
	return key != "" && len(key) <= 1024 && utf8.ValidString(key) && path.Clean(key) == key && !strings.HasPrefix(key, "/") && key != "." && key != ".." && !strings.HasPrefix(key, "../") && !strings.Contains(key, "\\") && strings.IndexFunc(key, unicode.IsControl) < 0
}
func ValidObjectContentType(value string) bool {
	kind, params, err := mime.ParseMediaType(value)
	major, minor, ok := strings.Cut(kind, "/")
	return err == nil && ok && major != "" && minor != "" && !strings.Contains(kind, "*") && mime.FormatMediaType(kind, params) == value
}

type StratusBucket struct {
	Name string `json:"bucket_name"`
	URL  string `json:"bucket_url"`
	Meta struct {
		Versioning *bool `json:"versioning"`
	} `json:"bucket_meta"`
}
type StratusObject struct {
	Key         string   `json:"key"`
	Size        *Integer `json:"size"`
	ContentType string   `json:"content_type"`
	ETag        string   `json:"etag"`
	VersionID   string   `json:"version_id"`
}
type stratusSignature struct {
	Query string
	Until time.Time
}

func stratusPath(project string) (string, error) {
	if !ValidID(project) {
		return "", errors.New("project ID must be a positive numeric string")
	}
	return "/baas/v1/project/" + project, nil
}
func stratusData(res *http.Response, target any) error {
	if res.StatusCode != 200 {
		return apiResponseError("Catalyst Stratus request", res)
	}
	var envelope struct {
		Status string          `json:"status"`
		Data   json.RawMessage `json:"data"`
	}
	if decode(res.Body, &envelope) != nil || envelope.Status != "success" || len(envelope.Data) == 0 || string(envelope.Data) == "null" || json.Unmarshal(envelope.Data, target) != nil {
		return errors.New("invalid Stratus API response")
	}
	return nil
}
func (c *Client) stratusOrigin(bucket string) string {
	suffix := map[string]string{"us": "com", "eu": "eu", "in": "in", "au": "com.au", "ca": "ca", "jp": "jp", "sa": "sa"}[c.config.DataCenter]
	environment := ""
	if c.config.Environment == "Development" {
		environment = "-development"
	}
	return "https://" + bucket + environment + ".zohostratus." + suffix
}
func (c *Client) GetStratusBucket(ctx context.Context, project, bucket string) (*StratusBucket, error) {
	base, err := stratusPath(project)
	if err != nil {
		return nil, err
	}
	if !ValidStratusBucketName(bucket) {
		return nil, errors.New("bucket must be an existing lowercase DNS label")
	}
	res, err := c.get(ctx, base+"/bucket?"+url.Values{"bucket_name": {bucket}}.Encode())
	if err != nil {
		return nil, err
	}
	var items []StratusBucket
	if res.StatusCode != http.StatusNotFound {
		err = stratusData(res, &items)
		if err == nil && items == nil {
			err = errors.New("bucket lookup omitted its result list")
		}
	}
	_ = res.Body.Close()
	if err != nil {
		return nil, err
	}
	if len(items) == 0 {
		if _, err = c.GetProject(ctx, project); err != nil {
			return nil, err
		}
		for attempt := 0; attempt < 3; attempt++ {
			buckets, err := c.ListBuckets(ctx, project)
			if err != nil {
				return nil, err
			}
			for _, item := range buckets {
				if item.Name == bucket {
					return nil, errors.New("bucket metadata is temporarily unavailable")
				}
			}
			if attempt < 2 {
				if err = c.wait(ctx, 2*time.Second); err != nil {
					return nil, err
				}
			}
		}
		return nil, errStratusBucketNotFound
	}
	if len(items) != 1 || items[0].Name != bucket || items[0].Meta.Versioning == nil || items[0].URL != c.stratusOrigin(bucket) {
		return nil, errors.New("incomplete bucket metadata or unexpected regional transfer origin")
	}
	if *items[0].Meta.Versioning {
		return nil, errors.New("versioned Stratus buckets are not supported by this object resource")
	}
	return &items[0], nil
}
func (c *Client) stratusSignature(ctx context.Context, project, bucket string) (string, error) {
	if err := ctx.Err(); err != nil {
		return "", err
	}
	select {
	case c.stratusSigns <- struct{}{}:
		defer func() { <-c.stratusSigns }()
	case <-ctx.Done():
		return "", ctx.Err()
	}
	if err := ctx.Err(); err != nil {
		return "", err
	}
	key := project + "/" + bucket
	if cached, ok := c.stratusSignatures[key]; ok && time.Now().Before(cached.Until) {
		return cached.Query, nil
	}
	base, err := stratusPath(project)
	if err != nil {
		return "", err
	}
	res, err := c.request(ctx, http.MethodPost, base+"/bucket/signature?"+url.Values{"bucket_name": {bucket}}.Encode(), nil)
	if err != nil {
		return "", err
	}
	defer res.Body.Close()
	var result struct {
		Signature string  `json:"signature"`
		Expiry    Integer `json:"expiry_time"`
	}
	if err = stratusData(res, &result); err != nil {
		return "", err
	}
	query, err := url.ParseQuery(strings.TrimPrefix(result.Signature, "?"))
	if err != nil || len(query) == 0 || len(result.Signature) > 8192 || int64(result.Expiry) <= time.Now().UnixMilli() {
		return "", errors.New("invalid or expired bucket signature")
	}
	encoded := query.Encode()
	if c.stratusSignatures == nil {
		c.stratusSignatures = map[string]stratusSignature{}
	}
	c.stratusSignatures[key] = stratusSignature{Query: encoded, Until: time.UnixMilli(int64(result.Expiry)).Add(-5 * time.Minute)}
	return encoded, nil
}

// signedObjectRequest never forwards OAuth or organization headers to the data host.
// The regional origin comes from a strict expected-host comparison, not user-supplied URLs.
func (c *Client) signedObjectRequest(ctx context.Context, project string, bucket *StratusBucket, key, method string, body []byte, contentType string, overwrite bool) (*http.Response, error) {
	signature, err := c.stratusSignature(ctx, project, bucket.Name)
	if err != nil {
		return nil, err
	}
	endpoint, err := url.Parse(c.stratusOrigin(bucket.Name))
	if err != nil {
		return nil, errors.New("invalid Stratus origin")
	}
	endpoint.Path = "/_signed/" + key
	endpoint.RawQuery = signature
	retries := 0
	for {
		req, err := http.NewRequestWithContext(ctx, method, endpoint.String(), bytes.NewReader(body))
		if err != nil {
			return nil, errors.New("could not construct Stratus transfer")
		}
		req.Header.Set("Accept-Encoding", "identity")
		req.Header.Set("Cache-Control", "no-cache")
		if method == http.MethodPut {
			req.Header.Set("Content-Type", contentType)
			req.Header.Set("compress", "false")
			req.Header.Set("overwrite", strconv.FormatBool(overwrite))
		}
		res, err := c.http.Do(req)
		if err != nil {
			if method == http.MethodGet && ctx.Err() == nil && retries < maxReadRetries {
				delay, _ := readRetryDelay(retries, "", time.Now())
				retries++
				if err = c.wait(ctx, delay); err == nil {
					continue
				}
			}
			return nil, requestError(ctx, "Stratus transfer")
		}
		if method == http.MethodGet && retryableReadStatus(res.StatusCode) && retries < maxReadRetries {
			delay, retry := readRetryDelay(retries, res.Header.Get("Retry-After"), time.Now())
			if retry {
				_ = res.Body.Close()
				retries++
				if err = c.wait(ctx, delay); err != nil {
					return nil, err
				}
				continue
			}
		}
		return res, nil
	}
}
func (c *Client) stratusObjectListed(ctx context.Context, project, bucket, key string) (bool, error) {
	base, err := stratusPath(project)
	if err != nil {
		return false, err
	}
	token := ""
	seen := map[string]bool{}
	keys := map[string]bool{}
	for page := 0; page < 100; page++ {
		query := url.Values{"bucket_name": {bucket}, "prefix": {key}, "folder_listing": {"false"}, "max_keys": {"100"}}
		if token != "" {
			query.Set("continuation_token", token)
		}
		res, err := c.get(ctx, base+"/bucket/objects?"+query.Encode())
		if err != nil {
			return false, err
		}
		var result struct {
			Contents []struct {
				Key string `json:"key"`
			} `json:"contents"`
			Truncated json.RawMessage `json:"truncated"`
			Next      string          `json:"next_continuation_token"`
		}
		err = stratusData(res, &result)
		_ = res.Body.Close()
		if err != nil {
			return false, err
		}
		if result.Contents == nil {
			return false, errors.New("the Stratus listing omitted object contents")
		}
		for _, item := range result.Contents {
			if !ValidStratusObjectKey(item.Key) || !strings.HasPrefix(item.Key, key) || keys[item.Key] {
				return false, errors.New("the Stratus listing has an invalid, duplicate or unexpected key")
			}
			keys[item.Key] = true
			if item.Key == key {
				return true, nil
			}
		}
		flag := strings.Trim(string(result.Truncated), `"`)
		if flag == "false" {
			return false, nil
		}
		if flag != "true" || result.Next == "" || seen[result.Next] {
			return false, errors.New("invalid Stratus continuation metadata")
		}
		seen[result.Next] = true
		token = result.Next
	}
	return false, errors.New("the Stratus absence verification exceeded 100 pages")
}
func (c *Client) confirmStratusObjectAbsent(ctx context.Context, project, bucket, key string) error {
	if _, err := c.GetStratusBucket(ctx, project, bucket); err != nil {
		if errors.Is(err, errStratusBucketNotFound) {
			return ErrStratusObjectNotFound
		}
		return err
	}
	for attempt := 0; attempt < 3; attempt++ {
		listed, err := c.stratusObjectListed(ctx, project, bucket, key)
		if err != nil {
			return err
		}
		if listed {
			return errStratusPending
		}
		if attempt < 2 {
			if err = c.wait(ctx, 2*time.Second); err != nil {
				return err
			}
		}
	}
	return ErrStratusObjectNotFound
}
func (c *Client) GetStratusObject(ctx context.Context, project, bucket, key string) (*StratusObject, error) {
	if !ValidStratusObjectKey(key) {
		return nil, errors.New("object key must be a safe nonempty path of at most 1024 UTF-8 bytes")
	}
	if _, err := c.GetStratusBucket(ctx, project, bucket); err != nil {
		if errors.Is(err, errStratusBucketNotFound) {
			return nil, ErrStratusObjectNotFound
		}
		return nil, err
	}
	base, err := stratusPath(project)
	if err != nil {
		return nil, err
	}
	res, err := c.get(ctx, base+"/bucket/object?"+url.Values{"bucket_name": {bucket}, "object_key": {key}}.Encode())
	if err != nil {
		return nil, err
	}
	defer res.Body.Close()
	if res.StatusCode == 404 {
		return nil, c.confirmStratusObjectAbsent(ctx, project, bucket, key)
	}
	var raw map[string]json.RawMessage
	if err = stratusData(res, &raw); err != nil {
		return nil, err
	}
	for field := range raw {
		switch field {
		case "key", "size", "content_type", "etag", "version_id", "last_modified", "object_url", "key_type":
		default:
			return nil, errors.New("the Stratus object has unrepresented metadata; refusing to manage it")
		}
	}
	encoded, _ := json.Marshal(raw)
	var result StratusObject
	if json.Unmarshal(encoded, &result) != nil {
		return nil, errors.New("invalid Stratus object metadata")
	}
	if result.Key != key || result.Size == nil || *result.Size < 0 || int64(*result.Size) > MaxFileBytes || !ValidObjectContentType(result.ContentType) || result.ETag == "" || result.VersionID != "" {
		return nil, errors.New("incomplete, versioned or unsupported Stratus object metadata")
	}
	return &result, nil
}

// PutStratusObject uses the service's overwrite=false guard for creation.
// A confirmed HTTP success is returned separately so callers retain identity before verification.
func (c *Client) PutStratusObject(ctx context.Context, project, bucket, key, contentType string, body []byte, creating bool) (bool, error) {
	if !ValidStratusObjectKey(key) || !ValidObjectContentType(contentType) || int64(len(body)) > MaxFileBytes {
		return false, errors.New("invalid Stratus key, content type or source larger than 100 MB")
	}
	details, err := c.GetStratusBucket(ctx, project, bucket)
	if err != nil {
		return false, err
	}
	_, err = c.GetStratusObject(ctx, project, bucket, key)
	if creating && err == nil {
		return false, errors.New("object key already exists; import it explicitly")
	}
	if creating && !errors.Is(err, ErrStratusObjectNotFound) {
		return false, err
	}
	if !creating && err != nil {
		return false, err
	}
	res, err := c.signedObjectRequest(ctx, project, details, key, http.MethodPut, body, contentType, !creating)
	if err != nil {
		return false, err
	}
	defer res.Body.Close()
	if res.StatusCode != 200 {
		return false, statusError("Stratus upload", res.StatusCode)
	}
	return true, nil
}
func (c *Client) StratusObjectSHA256(ctx context.Context, project, bucket, key string, expectedSize int64) (string, error) {
	if !ValidStratusObjectKey(key) || expectedSize < 0 || expectedSize > MaxFileBytes {
		return "", errors.New("invalid Stratus object key or size")
	}
	details, err := c.GetStratusBucket(ctx, project, bucket)
	if err != nil {
		if errors.Is(err, errStratusBucketNotFound) {
			return "", ErrStratusObjectNotFound
		}
		return "", err
	}
	res, err := c.signedObjectRequest(ctx, project, details, key, http.MethodGet, nil, "", false)
	if err != nil {
		return "", err
	}
	defer res.Body.Close()
	if res.StatusCode == 404 {
		_, err := c.GetStratusObject(ctx, project, bucket, key)
		if err != nil {
			return "", err
		}
		return "", errStratusPending
	}
	if res.StatusCode != 200 {
		return "", statusError("Stratus download", res.StatusCode)
	}
	hash := sha256.New()
	size, err := io.Copy(hash, io.LimitReader(res.Body, MaxFileBytes+1))
	if err != nil || size > MaxFileBytes {
		return "", errors.New("the Stratus download failed or exceeded its 100 MB limit")
	}
	if size != expectedSize {
		return "", errStratusPending
	}
	return hex.EncodeToString(hash.Sum(nil)), nil
}

// ReadStratusObject retries only identified propagation gaps. Permission failures,
// unsupported metadata and malformed responses retain state with an error.
func (c *Client) ReadStratusObject(ctx context.Context, project, bucket, key string) (*StratusObject, string, error) {
	ctx, cancel := context.WithTimeout(ctx, 2*time.Minute)
	defer cancel()
	for {
		current, err := c.GetStratusObject(ctx, project, bucket, key)
		if err == nil {
			var hash string
			hash, err = c.StratusObjectSHA256(ctx, project, bucket, key, int64(*current.Size))
			if err == nil {
				return current, hash, nil
			}
		}
		if !errors.Is(err, errStratusPending) {
			return nil, "", err
		}
		if err = c.wait(ctx, 2*time.Second); err != nil {
			return nil, "", err
		}
	}
}
func (c *Client) WaitStratusObject(ctx context.Context, project, bucket, key, contentType, hash string, size int64) (*StratusObject, error) {
	ctx, cancel := context.WithTimeout(ctx, 2*time.Minute)
	defer cancel()
	stable := 0
	for {
		current, actual, err := c.ReadStratusObject(ctx, project, bucket, key)
		if err != nil && !errors.Is(err, ErrStratusObjectNotFound) {
			return nil, err
		}
		matched := false
		if err == nil {
			matched = actual == hash && int64(*current.Size) == size && current.ContentType == contentType
		}
		if matched {
			stable++
			if stable == 3 {
				return current, nil
			}
		} else {
			stable = 0
		}
		if err = c.wait(ctx, 2*time.Second); err != nil {
			return nil, err
		}
	}
}
func (c *Client) DeleteStratusObject(ctx context.Context, project, bucket, key string) error {
	if _, err := c.GetStratusObject(ctx, project, bucket, key); err != nil {
		if errors.Is(err, ErrStratusObjectNotFound) {
			return nil
		}
		return err
	}
	base, err := stratusPath(project)
	if err != nil {
		return err
	}
	payload, _ := json.Marshal(map[string]any{"objects": []map[string]string{{"key": key}}})
	res, err := c.request(ctx, http.MethodPut, base+"/bucket/object?"+url.Values{"bucket_name": {bucket}}.Encode(), payload)
	if err != nil {
		return err
	}
	if res.StatusCode != 200 {
		err = apiResponseError("Stratus deletion", res)
		_ = res.Body.Close()
		return err
	}
	_ = res.Body.Close()
	ctx, cancel := context.WithTimeout(ctx, 2*time.Minute)
	defer cancel()
	for {
		_, err = c.GetStratusObject(ctx, project, bucket, key)
		if errors.Is(err, ErrStratusObjectNotFound) {
			return nil
		}
		if err != nil && !errors.Is(err, errStratusPending) {
			return err
		}
		if err = c.wait(ctx, 2*time.Second); err != nil {
			return err
		}
	}
}
