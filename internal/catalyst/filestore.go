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
	"mime/multipart"
	"net/http"
	"strings"
	"time"
	"unicode"
)

// MaxFileBytes is the published File Store upload limit (100 MB).
const MaxFileBytes int64 = 100_000_000

var ErrFileNotFound = errors.New("catalyst file does not exist")

type File struct {
	ID   ID       `json:"id"`
	Name string   `json:"file_name"`
	Size *Integer `json:"file_size"`
}

func ValidFilename(name string) bool {
	return strings.TrimSpace(name) != "" && name != "." && name != ".." && !strings.ContainsAny(name, "/\\") && strings.IndexFunc(name, unicode.IsControl) < 0
}
func filePath(projectID, folderID, id string) (string, error) {
	if !ValidID(projectID) || !ValidID(folderID) || (id != "" && !ValidID(id)) {
		return "", errors.New("project, folder and file IDs must be positive integer strings")
	}
	path := "/baas/v1/project/" + projectID + "/folder/" + folderID + "/file"
	if id != "" {
		path += "/" + id
	}
	return path, nil
}
func (c *Client) UploadFile(ctx context.Context, projectID, folderID, name string, content []byte) (*File, error) {
	path, err := filePath(projectID, folderID, "")
	if err != nil {
		return nil, err
	}
	if !ValidFilename(name) || int64(len(content)) > MaxFileBytes {
		return nil, errors.New("file name is invalid or file exceeds the 100 MB upload limit")
	}
	var body bytes.Buffer
	writer := multipart.NewWriter(&body)
	part, err := writer.CreateFormFile("code", name)
	if err != nil {
		return nil, errors.New("could not encode file upload")
	}
	if _, err = part.Write(content); err != nil {
		return nil, errors.New("could not encode file contents")
	}
	if err = writer.WriteField("file_name", name); err != nil {
		return nil, errors.New("could not encode file name")
	}
	if err = writer.Close(); err != nil {
		return nil, errors.New("could not finish file upload")
	}
	res, err := c.requestContent(ctx, http.MethodPost, path, body.Bytes(), writer.FormDataContentType())
	if err != nil {
		return nil, err
	}
	defer res.Body.Close()
	return c.readFile(ctx, res, projectID, "")
}
func (c *Client) GetFile(ctx context.Context, projectID, folderID, id string) (*File, error) {
	if !ValidID(id) {
		return nil, errors.New("invalid file ID")
	}
	path, err := filePath(projectID, folderID, id)
	if err != nil {
		return nil, err
	}
	res, err := c.get(ctx, path)
	if err != nil {
		return nil, err
	}
	defer res.Body.Close()
	return c.readFile(ctx, res, projectID, id)
}
func (c *Client) readFile(ctx context.Context, res *http.Response, projectID, id string) (*File, error) {
	if res.StatusCode != http.StatusOK && res.StatusCode != http.StatusCreated {
		return nil, c.fileError(ctx, res, projectID)
	}
	var envelope struct {
		Status string          `json:"status"`
		Data   json.RawMessage `json:"data"`
	}
	if decode(res.Body, &envelope) != nil || envelope.Status != "success" {
		return nil, errors.New("the Catalyst API returned an invalid file response")
	}
	// Recover the created identity before decoding metadata, so Terraform can retain
	// it for cleanup even when a successful upload's remaining fields are malformed.
	var identity struct {
		ID ID `json:"id"`
	}
	if json.Unmarshal(envelope.Data, &identity) != nil || !ValidID(string(identity.ID)) || (id != "" && string(identity.ID) != id) {
		return nil, errors.New("the Catalyst API returned an invalid file identity")
	}
	file := &File{ID: identity.ID}
	if json.Unmarshal(envelope.Data, file) != nil || !ValidFilename(file.Name) || file.Size == nil || *file.Size < 0 {
		return &File{ID: identity.ID}, errors.New("the Catalyst API returned incomplete file metadata")
	}
	return file, nil
}
func (c *Client) fileError(ctx context.Context, res *http.Response, projectID string) error {
	var failure apiFailure
	if decode(res.Body, &failure) == nil && res.StatusCode == http.StatusNotFound && failure.Status == "failure" && failure.Data.Code == "INVALID_ID" {
		// Require access to both the project and folder inventory before interpreting
		// a missing child as absence. Permission failures never erase managed state.
		if _, err := c.GetProject(ctx, projectID); err != nil {
			return err
		}
		if _, err := c.ListFolders(ctx, projectID); err != nil {
			return err
		}
		return ErrFileNotFound
	}
	return failureError("Catalyst file request", res.StatusCode, failure)
}
func (c *Client) FileSHA256(ctx context.Context, projectID, folderID, id string) (string, error) {
	if !ValidID(id) {
		return "", errors.New("invalid file ID")
	}
	path, err := filePath(projectID, folderID, id)
	if err != nil {
		return "", err
	}
	res, err := c.get(ctx, path+"/download")
	if err != nil {
		return "", err
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusOK {
		return "", c.fileError(ctx, res, projectID)
	}
	hash := sha256.New()
	size, err := io.Copy(hash, io.LimitReader(res.Body, MaxFileBytes+1))
	if err != nil || size > MaxFileBytes {
		return "", errors.New("could not read file contents within the 100 MB limit")
	}
	return hex.EncodeToString(hash.Sum(nil)), nil
}
func (c *Client) DeleteFile(ctx context.Context, projectID, folderID, id string) error {
	if !ValidID(id) {
		return errors.New("invalid file ID")
	}
	path, err := filePath(projectID, folderID, id)
	if err != nil {
		return err
	}
	res, err := c.request(ctx, http.MethodDelete, path, nil)
	if err != nil {
		return err
	}
	defer res.Body.Close()
	_, err = c.readFile(ctx, res, projectID, id)
	return err
}
func (c *Client) WaitFileDeleted(ctx context.Context, projectID, folderID, id string) error {
	ctx, cancel := context.WithTimeout(ctx, 2*time.Minute)
	defer cancel()
	for {
		_, err := c.GetFile(ctx, projectID, folderID, id)
		if errors.Is(err, ErrFileNotFound) {
			return nil
		}
		if err != nil {
			return err
		}
		if err = c.wait(ctx, 2*time.Second); err != nil {
			return err
		}
	}
}
