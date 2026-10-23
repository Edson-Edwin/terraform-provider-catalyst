// SPDX-License-Identifier: MPL-2.0

package catalyst

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"
)

func TestAccRegionalStratusRead(t *testing.T) {
	runRegionalStratus(t, regionalStratusRead)
}

func TestAccRegionalStratusLifecycle(t *testing.T) {
	runRegionalStratus(t, regionalStratusWrite)
}

func runRegionalStratus(t *testing.T, check regionalCheck) {
	t.Helper()
	if os.Getenv("TF_ACC") != "1" {
		t.Skip("set TF_ACC=1 and explicit regional Stratus fixtures")
	}
	fixtures, err := loadRegionalFixtures(os.Getenv, check)
	if err != nil {
		t.Fatal(err)
	}
	for _, f := range fixtures {
		t.Run(f.name, func(t *testing.T) {
			c, err := New(f.config, nil)
			if err != nil {
				t.Fatal(err)
			}
			project, err := c.GetProject(t.Context(), f.project)
			if err != nil {
				t.Fatal(err)
			}
			if string(project.ID) != f.project {
				t.Fatal("project response did not match the requested fixture identity")
			}
			if check == regionalStratusRead {
				err = verifyRegionalStratusRead(t.Context(), c, f)
			} else {
				var nonce [16]byte
				if _, err = rand.Read(nonce[:]); err != nil {
					t.Fatal("could not generate a unique disposable object identity")
				}
				key := "tfacc-regional/" + hex.EncodeToString(nonce[:]) + ".bin"
				// This generated identity is safe to log and permits exact-key
				// recovery after process termination. Never log signed URLs.
				t.Logf("Disposable object key: %s", key)
				err = verifyRegionalStratusLifecycle(t.Context(), c, f, key)
			}
			if err != nil {
				t.Fatal(err)
			}
		})
	}
}

func verifyRegionalStratusRead(ctx context.Context, c *Client, f regionalFixture) error {
	_, hash, err := c.ReadStratusObject(ctx, f.project, f.bucket, f.key)
	if err != nil {
		return fmt.Errorf("regional Stratus metadata/signed download failed: %w", err)
	}
	if hash != f.sha256 {
		return errors.New("regional Stratus fixture SHA-256 mismatch")
	}
	return nil
}

func verifyRegionalStratusLifecycle(ctx context.Context, c *Client, f regionalFixture, key string) (resultErr error) {
	if !strings.HasPrefix(key, "tfacc-regional/") || !ValidStratusObjectKey(key) || key == f.key {
		return errors.New("invalid disposable object identity")
	}
	// Verify the known fixture before any mutation, including signed routing.
	if err := verifyRegionalStratusRead(ctx, c, f); err != nil {
		return err
	}
	if _, err := c.GetStratusObject(ctx, f.project, f.bucket, key); !errors.Is(err, ErrStratusObjectNotFound) {
		return errors.New("disposable key absence unverified; no write or cleanup attempted")
	}
	body := regionalStratusPayload(key)
	hash := sha256.Sum256(body)
	wantHash := hex.EncodeToString(hash[:])
	// Own only the previously absent generated key. Register cleanup before the
	// first potentially ambiguous upload; cancellation cannot prevent cleanup.
	defer func() {
		cleanupCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 3*time.Minute)
		defer cancel()
		_, actualHash, err := c.ReadStratusObject(cleanupCtx, f.project, f.bucket, key)
		if errors.Is(err, ErrStratusObjectNotFound) {
			return
		}
		if err != nil || actualHash != wantHash {
			resultErr = errors.Join(resultErr, fmt.Errorf("cleanup ownership unverified for generated key %s; object retained", key))
			return
		}
		if err := c.DeleteStratusObject(cleanupCtx, f.project, f.bucket, key); err != nil {
			resultErr = errors.Join(resultErr, fmt.Errorf("cleanup failed for generated key %s: %w", key, err))
			return
		}
		if _, err := c.GetStratusObject(cleanupCtx, f.project, f.bucket, key); !errors.Is(err, ErrStratusObjectNotFound) {
			resultErr = errors.Join(resultErr, fmt.Errorf("cleanup absence unverified for generated key %s", key))
		}
	}()
	accepted, err := c.PutStratusObject(ctx, f.project, f.bucket, key, "application/octet-stream", body, true)
	if err != nil {
		return fmt.Errorf("disposable Stratus upload failed: %w", err)
	}
	if !accepted {
		return errors.New("disposable Stratus upload was not confirmed")
	}
	if _, err := c.WaitStratusObject(ctx, f.project, f.bucket, key, "application/octet-stream", wantHash, int64(len(body))); err != nil {
		return fmt.Errorf("disposable Stratus content verification failed: %w", err)
	}
	return nil
}

func regionalStratusPayload(key string) []byte {
	// Bind content to the random identity, so cleanup cannot delete a different
	// object that won a create race or replaced an ambiguous upload.
	return append([]byte{0, 255, 1, 2, 3}, []byte(key)...)
}
