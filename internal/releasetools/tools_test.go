// SPDX-License-Identifier: MPL-2.0
package releasetools

import (
	"archive/zip"
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

func fixtureTools(t *testing.T) (*Tools, string) {
	t.Helper()
	root := t.TempDir()
	if err := os.Mkdir(filepath.Join(root, "coverage"), 0700); err != nil {
		t.Fatal(err)
	}
	writeFixture(t, filepath.Join(root, "coverage", "capabilities.json"), []byte(`{"inventory_complete":false,"capabilities":[{"id":"fixture","status":"managed","resources":["catalyst_fixture"],"data_sources":["catalyst_fixtures"]}]}`))
	tool := New(root)
	tool.Native = "linux_amd64"
	tool.Run = func(context.Context, string, []string, string, ...string) ([]byte, error) {
		t.Fatal("rejected package/arguments executed a command")
		return nil, nil
	}
	return tool, t.TempDir()
}
func writeFixture(t *testing.T, path string, raw []byte) {
	t.Helper()
	if err := os.WriteFile(path, raw, 0600); err != nil {
		t.Fatal(err)
	}
}
func writeSums(t *testing.T, dir, version string) {
	t.Helper()
	files, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	var sums strings.Builder
	for _, file := range files {
		if strings.HasSuffix(file.Name(), "_SHA256SUMS") || strings.HasSuffix(file.Name(), ".sig") {
			continue
		}
		raw, err := os.ReadFile(filepath.Join(dir, file.Name()))
		if err != nil {
			t.Fatal(err)
		}
		hash := sha256.Sum256(raw)
		fmt.Fprintf(&sums, "%s  %s\n", hex.EncodeToString(hash[:]), file.Name())
	}
	writeFixture(t, filepath.Join(dir, "terraform-provider-catalyst_"+version+"_SHA256SUMS"), []byte(sums.String()))
}
func releaseFixture(t *testing.T, dir, version string) {
	t.Helper()
	for _, target := range targets {
		var buffer bytes.Buffer
		writer := zip.NewWriter(&buffer)
		name := "terraform-provider-catalyst_v" + version
		if strings.HasPrefix(target, "windows_") {
			name += ".exe"
		}
		file, err := writer.Create(name)
		if err != nil {
			t.Fatal(err)
		}
		if _, err = file.Write([]byte("fixture; never execute")); err != nil {
			t.Fatal(err)
		}
		if err = writer.Close(); err != nil {
			t.Fatal(err)
		}
		writeFixture(t, filepath.Join(dir, "terraform-provider-catalyst_"+version+"_"+target+".zip"), buffer.Bytes())
	}
	writeFixture(t, filepath.Join(dir, "terraform-provider-catalyst_"+version+"_manifest.json"), []byte(`{"version":1,"metadata":{"protocol_versions":["6.0"]}}`))
	writeSums(t, dir, version)
}
func TestReleaseRejectsBeforeExecution(t *testing.T) {
	for _, scenario := range []string{"mixed_versions", "wrong_version", "corrupt_artifact", "unsupported_manifest", "uppercase_prerelease", "unsafe_checksum_path", "missing_platform", "unchecksummed_archive"} {
		t.Run(scenario, func(t *testing.T) {
			tool, dir := fixtureTools(t)
			version := "1.0.0"
			if scenario == "uppercase_prerelease" {
				version = "1.0.0-ALPHA"
			}
			releaseFixture(t, dir, version)
			expected := ""
			switch scenario {
			case "mixed_versions":
				if err := os.Rename(filepath.Join(dir, "terraform-provider-catalyst_1.0.0_linux_arm.zip"), filepath.Join(dir, "terraform-provider-catalyst_1.0.1_linux_arm.zip")); err != nil {
					t.Fatal(err)
				}
				writeSums(t, dir, version)
			case "wrong_version":
				expected = "2.0.0"
			case "corrupt_artifact":
				writeFixture(t, filepath.Join(dir, "terraform-provider-catalyst_1.0.0_linux_arm.zip"), []byte("changed"))
			case "unsupported_manifest":
				writeFixture(t, filepath.Join(dir, "terraform-provider-catalyst_1.0.0_manifest.json"), []byte(`{"version":2,"metadata":{"protocol_versions":["6.0"]}}`))
				writeSums(t, dir, version)
			case "unsafe_checksum_path":
				writeFixture(t, filepath.Join(dir, "terraform-provider-catalyst_1.0.0_SHA256SUMS"), []byte(strings.Repeat("0", 64)+"  ../escape\n"))
			case "missing_platform":
				if err := os.Remove(filepath.Join(dir, "terraform-provider-catalyst_1.0.0_linux_arm.zip")); err != nil {
					t.Fatal(err)
				}
				writeSums(t, dir, version)
			case "unchecksummed_archive":
				writeFixture(t, filepath.Join(dir, "unlisted.zip"), []byte("fixture"))
			}
			if err := tool.VerifyRelease(t.Context(), dir, expected, ""); err == nil {
				t.Fatal("invalid release accepted")
			}
		})
	}
}
func schemaFixture() []byte {
	return []byte(`{"provider_schemas":{"registry.terraform.io/edson-edwin/catalyst":{"resource_schemas":{"catalyst_fixture":{}},"data_source_schemas":{"catalyst_fixtures":{}}}}}`)
}
func TestReleaseUsesOnlyFilesystemMirror(t *testing.T) {
	tool, dir := fixtureTools(t)
	releaseFixture(t, dir, "1.0.0")
	t.Setenv("TF_CLI_ARGS", "-invalid")
	t.Setenv("CATALYST_ACCESS_TOKEN", "test-secret")
	calls := 0
	tool.Run = func(ctx context.Context, root string, env []string, name string, args ...string) ([]byte, error) {
		calls++
		config, err := os.ReadFile(filepath.Join(root, "terraform.rc"))
		if err != nil {
			t.Fatal(err)
		}
		if !bytesContain(config, []byte("filesystem_mirror")) || bytesContain(config, []byte("dev_overrides")) || bytesContain(config, []byte("direct")) {
			t.Fatal("mirror isolation lost")
		}
		hcl, err := os.ReadFile(filepath.Join(root, "main.tf"))
		if err != nil {
			t.Fatal(err)
		}
		if !bytesContain(hcl, []byte(`version = "1.0.0"`)) {
			t.Fatal("version not pinned")
		}
		for _, v := range env {
			if strings.HasPrefix(v, "TF_CLI_ARGS=") || strings.HasPrefix(v, "CATALYST_") {
				t.Fatal("inherited environment leaked")
			}
		}
		return schemaFixture(), nil
	}
	if err := tool.VerifyRelease(t.Context(), dir, "1.0.0", ""); err != nil {
		t.Fatal(err)
	}
	if calls != 2 {
		t.Fatal("expected init and schema handshake")
	}
}
func TestRegistryRejectsNonExactVersions(t *testing.T) {
	tool, _ := fixtureTools(t)
	for _, version := range []string{"latest", "v1.0.0", "1.0", "1.0.0-ALPHA", "../1.0.0", "01.0.0"} {
		if err := tool.VerifyRegistry(t.Context(), version); err == nil {
			t.Fatal("unsafe version accepted")
		}
	}
}
func TestRegistryUsesOnlyDirectInstall(t *testing.T) {
	tool, _ := fixtureTools(t)
	for _, key := range []string{"TF_REATTACH_PROVIDERS", "CATALYST_ACCESS_TOKEN", "TF_PLUGIN_CACHE_DIR", "TF_DATA_DIR", "TF_CLI_ARGS"} {
		t.Setenv(key, "injected")
	}
	calls := 0
	tool.Run = func(ctx context.Context, root string, env []string, name string, args ...string) ([]byte, error) {
		calls++
		config, err := os.ReadFile(filepath.Join(root, "terraform.rc"))
		if err != nil {
			t.Fatal(err)
		}
		if !bytesContain(config, []byte("direct")) || bytesContain(config, []byte("mirror")) || bytesContain(config, []byte("dev_overrides")) {
			t.Fatal("public install has fallback")
		}
		for _, v := range env {
			if strings.Contains(v, "injected") {
				t.Fatal("inherited override/credential leaked")
			}
		}
		if args[0] == "init" {
			writeFixture(t, filepath.Join(root, ".terraform.lock.hcl"), []byte("version = \"1.2.3\"\nhashes = [\"zh:fixture\"]"))
		}
		return schemaFixture(), nil
	}
	if err := tool.VerifyRegistry(t.Context(), "1.2.3"); err != nil {
		t.Fatal(err)
	}
	if calls != 2 {
		t.Fatal("expected init and schema handshake")
	}
}
func TestSignatureIdentityAndCommandFailure(t *testing.T) {
	for _, scenario := range []string{"wrong_key", "primary_signing_subkey", "failed_command", "missing_signature"} {
		t.Run(scenario, func(t *testing.T) {
			tool, dir := fixtureTools(t)
			sums := filepath.Join(dir, "fixture_SHA256SUMS")
			writeFixture(t, sums, []byte("fixture"))
			if scenario != "missing_signature" {
				writeFixture(t, sums+".sig", []byte("fixture"))
			}
			calls := 0
			tool.Run = func(ctx context.Context, root string, env []string, name string, args ...string) ([]byte, error) {
				calls++
				if name != "gpg" || !slices.Contains(args, "--no-auto-key-retrieve") {
					t.Fatal("signature check allows network key lookup")
				}
				if scenario == "failed_command" {
					return nil, errors.New("failed")
				}
				primary := strings.Repeat("B", 40)
				if scenario == "primary_signing_subkey" {
					primary = strings.Repeat("A", 40)
				}
				return []byte("[GNUPG:] VALIDSIG " + strings.Repeat("B", 40) + " 2026-10-23 0 0 4 0 1 8 00 " + primary), nil
			}
			err := tool.VerifySignature(t.Context(), dir, strings.Repeat("A", 40))
			if (err == nil) != (scenario == "primary_signing_subkey") {
				t.Fatal("signature identity/failure classified incorrectly")
			}
			if scenario == "missing_signature" && calls != 0 {
				t.Fatal("missing signature executed a command")
			}
		})
	}
}
func TestSignatureFailureStopsBeforeArtifactLaunch(t *testing.T) {
	tool, dir := fixtureTools(t)
	writeFixture(t, filepath.Join(dir, "fixture_SHA256SUMS"), []byte("fixture"))
	writeFixture(t, filepath.Join(dir, "fixture_SHA256SUMS.sig"), []byte("fixture"))
	calls := 0
	tool.Run = func(ctx context.Context, root string, env []string, name string, args ...string) ([]byte, error) {
		calls++
		if name != "gpg" {
			t.Fatal("artifact launched before signature verified")
		}
		return []byte("[GNUPG:] VALIDSIG " + strings.Repeat("B", 40)), nil
	}
	if err := tool.VerifyRelease(t.Context(), dir, "", strings.Repeat("A", 40)); err == nil || calls != 1 {
		t.Fatal("signature gate bypassed")
	}
}
func TestCoverageReportsCountsWithoutFullCoverageClaim(t *testing.T) {
	tool, _ := fixtureTools(t)
	var output bytes.Buffer
	if err := tool.Coverage(&output); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(output.String(), "managed: 1") || !strings.Contains(output.String(), "10/10 coverage: not established") {
		t.Fatal("coverage claim/count incorrect")
	}
}
func TestSnapshotStagingRestrictedToSnapshots(t *testing.T) {
	tool, dir := fixtureTools(t)
	writeFixture(t, filepath.Join(tool.Root, "terraform-registry-manifest.json"), []byte(`{"version":1}`))
	for _, version := range []string{"1.0.0", "../bad", "1.0.0-dev.abc123"} {
		raw, err := json.Marshal(map[string]string{"project_name": "terraform-provider-catalyst", "version": version})
		if err != nil {
			t.Fatal(err)
		}
		writeFixture(t, filepath.Join(dir, "metadata.json"), raw)
		err = tool.StageSnapshot(dir)
		if (err == nil) != (version == "1.0.0-dev.abc123") {
			t.Fatal("non-snapshot manifest staging accepted")
		}
	}
}

func TestReleaseZipRejectsUnsafeAndAmbiguousEntries(t *testing.T) {
	for _, entry := range []string{"..", ".", "C:escape", "../escape", "/absolute", `dir\file`, "duplicate", "terraform-provider-catalyst_vother"} {
		t.Run(entry, func(t *testing.T) {
			var buffer bytes.Buffer
			writer := zip.NewWriter(&buffer)
			binary := "terraform-provider-catalyst_v1.0.0"
			for _, name := range []string{binary, entry} {
				file, err := writer.Create(name)
				if err != nil {
					t.Fatal(err)
				}
				if _, err = file.Write([]byte("fixture")); err != nil {
					t.Fatal(err)
				}
			}
			if entry == "duplicate" {
				if _, err := writer.Create(entry); err != nil {
					t.Fatal(err)
				}
			}
			if err := writer.Close(); err != nil {
				t.Fatal(err)
			}
			path := filepath.Join(t.TempDir(), "fixture.zip")
			writeFixture(t, path, buffer.Bytes())
			if err := verifyZip(path, binary); err == nil {
				t.Fatal("unsafe/ambiguous archive accepted")
			}
		})
	}
}
