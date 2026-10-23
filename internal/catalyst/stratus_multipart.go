// SPDX-License-Identifier: MPL-2.0
package catalyst

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"regexp"
	"strconv"
	"strings"
)

var stratusUploadID = regexp.MustCompile(`^[A-Za-z0-9_-]{1,200}$`)

// PutStratusObjectMultipart follows the official SDK's initiate/part/complete
// protocol. Each mutation is submitted once. The existing 100 MiB artifact limit
// and unversioned-bucket ownership rules still apply.
func (c *Client) PutStratusObjectMultipart(ctx context.Context, project, bucket, key, contentType string, body []byte, creating bool, partMiB int64) (bool, error) {
	if !ValidStratusObjectKey(key) || !ValidObjectContentType(contentType) || len(body) == 0 || int64(len(body)) > MaxFileBytes || partMiB < 5 || partMiB > 20 {
		return false, errors.New("multipart upload requires a nonempty source up to 100 MiB and a part size of 5–20 MiB")
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
	res, err := c.signedStratusRequest(ctx, project, details, key, http.MethodPut, nil, contentType, !creating, url.Values{"multipart": {""}})
	if err != nil {
		return false, err
	}
	if res.StatusCode != 200 {
		status := res.StatusCode
		_ = res.Body.Close()
		return false, statusError("Stratus multipart initiation", status)
	}
	var init struct {
		Bucket   string `json:"bucket"`
		Key      string `json:"key"`
		UploadID string `json:"upload_id"`
		Status   string `json:"status"`
	}
	err = decode(res.Body, &init)
	_ = res.Body.Close()
	expectedBucket := strings.Split(strings.TrimPrefix(details.URL, "https://"), ".")[0]
	if err != nil || init.Key != key || init.Bucket != expectedBucket || !stratusUploadID.MatchString(init.UploadID) || init.Status != "PENDING" {
		return false, errors.New("invalid multipart initiation response; inspect incomplete uploads before retrying")
	}
	partBytes := int(partMiB * 1024 * 1024)
	for offset, part := 0, 1; offset < len(body); offset, part = offset+partBytes, part+1 {
		end := min(offset+partBytes, len(body))
		r, err := c.signedStratusRequest(ctx, project, details, key, http.MethodPut, body[offset:end], contentType, false, url.Values{"uploadId": {init.UploadID}, "partNumber": {strconv.Itoa(part)}})
		if err != nil {
			return false, fmt.Errorf("multipart session %s is incomplete: %w", init.UploadID, err)
		}
		status := r.StatusCode
		_ = r.Body.Close()
		if status != 200 {
			return false, fmt.Errorf("multipart session %s is incomplete: %w", init.UploadID, statusError("Stratus part upload", status))
		}
	}
	r, err := c.signedStratusRequest(ctx, project, details, key, http.MethodPut, nil, contentType, !creating, url.Values{"completeMultipart": {""}, "uploadId": {init.UploadID}})
	// Completion may be committed even when its response is lost. Retain the
	// exact object key in Terraform for recovery instead of replaying completion.
	if err != nil {
		return true, fmt.Errorf("multipart session %s completion is uncertain: %w", init.UploadID, err)
	}
	status := r.StatusCode
	_ = r.Body.Close()
	if status != http.StatusAccepted {
		return status >= 500 || status == http.StatusRequestTimeout, fmt.Errorf("multipart session %s: %w", init.UploadID, statusError("Stratus multipart completion", status))
	}
	return true, nil
}
