// SPDX-License-Identifier: MPL-2.0
// Package releasetools verifies source and release evidence without publishing.
package releasetools

import (
	"archive/zip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"regexp"
	"runtime"
	"slices"
	"strconv"
	"strings"
	"time"
)

const providerAddress = "registry.terraform.io/edson-edwin/catalyst"

var exactVersion = regexp.MustCompile(`^(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)(?:-[0-9a-z]+(?:[.-][0-9a-z]+)*)?$`)
var targets = []string{"linux_amd64", "linux_arm64", "linux_386", "linux_arm", "linux_s390x", "darwin_amd64", "darwin_arm64", "windows_amd64", "windows_arm64", "windows_386", "freebsd_amd64", "freebsd_arm64", "freebsd_386", "freebsd_arm"}

type Runner func(context.Context, string, []string, string, ...string) ([]byte, error)
type Tools struct {
	Root   string
	Run    Runner
	Native string
}

func New(root string) *Tools {
	return &Tools{Root: root, Run: runCommand, Native: runtime.GOOS + "_" + runtime.GOARCH}
}
func runCommand(ctx context.Context, dir string, env []string, name string, args ...string) ([]byte, error) {
	cmd := exec.CommandContext(ctx, name, args...)
	cmd.Dir = dir
	cmd.Env = env
	output, err := cmd.Output()
	if err != nil {
		return nil, fmt.Errorf("%s verification command failed; output withheld", name)
	}
	return output, nil
}
func (t *Tools) command(ctx context.Context, timeout time.Duration, dir string, env []string, name string, args ...string) ([]byte, error) {
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	return t.Run(ctx, dir, env, name, args...)
}
func isolatedEnv(config string) []string {
	result := []string{}
	for _, value := range os.Environ() {
		key, _, _ := strings.Cut(value, "=")
		if !strings.HasPrefix(key, "TF_") && !strings.HasPrefix(key, "CATALYST_") && key != "CHECKPOINT_DISABLE" {
			result = append(result, value)
		}
	}
	return append(result, "TF_CLI_CONFIG_FILE="+config, "CHECKPOINT_DISABLE=1", "TF_IN_AUTOMATION=1")
}

type Capability struct {
	ID          string   `json:"id"`
	Status      string   `json:"status"`
	Resources   []string `json:"resources"`
	DataSources []string `json:"data_sources"`
}
type Inventory struct {
	Complete     bool         `json:"inventory_complete"`
	Capabilities []Capability `json:"capabilities"`
}

func (t *Tools) inventory() (Inventory, error) {
	var inventory Inventory
	raw, err := os.ReadFile(filepath.Join(t.Root, "coverage", "capabilities.json"))
	if err != nil {
		return inventory, err
	}
	if err = json.Unmarshal(raw, &inventory); err != nil {
		return inventory, err
	}
	if len(inventory.Capabilities) == 0 {
		return inventory, errors.New("empty capability inventory")
	}
	return inventory, nil
}
func (t *Tools) Coverage(w io.Writer) error {
	inventory, err := t.inventory()
	if err != nil {
		return err
	}
	counts := map[string]int{}
	resources, sources := 0, 0
	for _, entry := range inventory.Capabilities {
		counts[entry.Status]++
		resources += len(entry.Resources)
		sources += len(entry.DataSources)
	}
	label := "draft; service/field audit incomplete"
	if inventory.Complete {
		label = "complete"
	}
	_, err = fmt.Fprintf(w, "Inventory: %s\nCapabilities: %d\nmanaged: %d\ndiscovery: %d\nunimplemented: %d\nTerraform resources: %d\nTerraform data sources: %d\n10/10 coverage: not established.\n", label, len(inventory.Capabilities), counts["managed"], counts["discovery"], counts["unimplemented"], resources, sources)
	return err
}
func (t *Tools) verifySchema(raw []byte, exact bool) error {
	var response struct {
		Providers map[string]struct {
			Resources map[string]json.RawMessage `json:"resource_schemas"`
			Sources   map[string]json.RawMessage `json:"data_source_schemas"`
		} `json:"provider_schemas"`
	}
	if json.Unmarshal(raw, &response) != nil {
		return errors.New("invalid Terraform schema response")
	}
	schema, ok := response.Providers[providerAddress]
	if !ok {
		return errors.New("provider schema missing")
	}
	inventory, err := t.inventory()
	if err != nil {
		return err
	}
	resourceCount, sourceCount := 0, 0
	for _, entry := range inventory.Capabilities {
		for _, name := range entry.Resources {
			resourceCount++
			if _, ok := schema.Resources[name]; !ok {
				return errors.New("packaged provider is missing a required resource")
			}
		}
		for _, name := range entry.DataSources {
			sourceCount++
			if _, ok := schema.Sources[name]; !ok {
				return errors.New("packaged provider is missing a required data source")
			}
		}
	}
	if exact && (len(schema.Resources) != resourceCount || len(schema.Sources) != sourceCount) {
		return errors.New("packaged provider has unexpected Terraform types")
	}
	return nil
}
func (t *Tools) install(ctx context.Context, root, version, mirror string, public bool) error {
	config := filepath.Join(root, "terraform.rc")
	policy := "direct"
	if !public {
		policy = "filesystem_mirror"
	}
	body := "provider_installation {\n " + policy + " {\n"
	if !public {
		body += " path = " + strconv.Quote(mirror) + "\n"
	}
	body += " include = [\"" + providerAddress + "\"]\n }\n}\ndisable_checkpoint = true\n"
	if err := os.WriteFile(config, []byte(body), 0600); err != nil {
		return err
	}
	hcl := "terraform {\n required_providers {\n catalyst = { source = \"Edson-Edwin/catalyst\", version = " + strconv.Quote(version) + " }\n }\n}\n"
	if err := os.WriteFile(filepath.Join(root, "main.tf"), []byte(hcl), 0600); err != nil {
		return err
	}
	env := isolatedEnv(config)
	timeout := time.Minute
	if public {
		timeout = 3 * time.Minute
	}
	if _, err := t.command(ctx, timeout, root, env, "terraform", "init", "-backend=false", "-input=false", "-no-color"); err != nil {
		return err
	}
	if public {
		raw, err := os.ReadFile(filepath.Join(root, ".terraform.lock.hcl"))
		if err != nil {
			return err
		}
		pattern := regexp.MustCompile(`version\s*=\s*"` + regexp.QuoteMeta(version) + `"`)
		if !pattern.Match(raw) || !bytesContain(raw, []byte("zh:")) {
			return errors.New("installed provider version/archive hashes missing from lock file")
		}
	}
	raw, err := t.command(ctx, time.Minute, root, env, "terraform", "providers", "schema", "-json")
	if err != nil {
		return err
	}
	return t.verifySchema(raw, !public)
}
func bytesContain(a, b []byte) bool { return strings.Contains(string(a), string(b)) }
func (t *Tools) VerifyRegistry(ctx context.Context, version string) error {
	if !exactVersion.MatchString(version) {
		return errors.New("use an exact lowercase semantic version without v")
	}
	root, err := os.MkdirTemp("", "catalyst-registry-check-")
	if err != nil {
		return err
	}
	defer os.RemoveAll(root)
	return t.install(ctx, root, version, "", true)
}
func (t *Tools) StageSnapshot(dir string) error {
	raw, err := os.ReadFile(filepath.Join(dir, "metadata.json"))
	if err != nil {
		return err
	}
	var metadata struct {
		Project string `json:"project_name"`
		Version string `json:"version"`
	}
	if json.Unmarshal(raw, &metadata) != nil || metadata.Project != "terraform-provider-catalyst" || !regexp.MustCompile(`^[0-9]+\.[0-9]+\.[0-9]+-dev\.[0-9a-f]+$`).MatchString(metadata.Version) {
		return errors.New("manifest staging is allowed only for local snapshots")
	}
	raw, err = os.ReadFile(filepath.Join(t.Root, "terraform-registry-manifest.json"))
	if err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(dir, "terraform-provider-catalyst_"+metadata.Version+"_manifest.json"), raw, 0600)
}
func (t *Tools) VerifySignature(ctx context.Context, dir, fingerprint string) error {
	fingerprint = strings.ToUpper(fingerprint)
	if !regexp.MustCompile(`^(?:[0-9A-F]{40}|[0-9A-F]{64})$`).MatchString(fingerprint) {
		return errors.New("provide the full signing-key fingerprint")
	}
	sums, err := filepath.Glob(filepath.Join(dir, "*_SHA256SUMS"))
	if err != nil || len(sums) != 1 {
		return errors.New("expected one checksum file")
	}
	if _, err = os.Stat(sums[0] + ".sig"); err != nil {
		return errors.New("expected a detached checksum signature")
	}
	output, err := t.command(ctx, 30*time.Second, t.Root, nil, "gpg", "--batch", "--no-auto-key-retrieve", "--status-fd=1", "--verify", sums[0]+".sig", sums[0])
	if err != nil {
		return errors.New("checksum signature verification failed")
	}
	for _, line := range strings.Split(string(output), "\n") {
		fields := strings.Fields(line)
		if len(fields) >= 3 && fields[0] == "[GNUPG:]" && fields[1] == "VALIDSIG" && (fields[2] == fingerprint || fields[len(fields)-1] == fingerprint) {
			return nil
		}
	}
	return errors.New("checksum signature is invalid or belongs to a different signing key")
}
func copyFile(source, target string) error {
	input, err := os.Open(source)
	if err != nil {
		return err
	}
	defer input.Close()
	output, err := os.OpenFile(target, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
	if err != nil {
		return err
	}
	_, copyErr := io.Copy(output, input)
	closeErr := output.Close()
	if copyErr != nil {
		return copyErr
	}
	return closeErr
}
func (t *Tools) VerifyRelease(ctx context.Context, dir, expectedVersion, fingerprint string) error {
	if fingerprint != "" {
		if err := t.VerifySignature(ctx, dir, fingerprint); err != nil {
			return err
		}
	}
	sums, err := filepath.Glob(filepath.Join(dir, "*_SHA256SUMS"))
	if err != nil || len(sums) != 1 {
		return errors.New("expected one checksum file")
	}
	raw, err := os.ReadFile(sums[0])
	if err != nil {
		return err
	}
	entries := map[string]string{}
	for _, line := range strings.Split(strings.TrimSpace(string(raw)), "\n") {
		fields := strings.Fields(line)
		if len(fields) != 2 || !regexp.MustCompile(`^[0-9a-f]{64}$`).MatchString(fields[0]) || fields[1] == "." || strings.ContainsAny(fields[1], "/\\:") || fields[1] == ".." {
			return errors.New("invalid checksum entry")
		}
		name := fields[1]
		if _, ok := entries[name]; ok {
			return errors.New("duplicate checksum entry")
		}
		artifact := filepath.Join(dir, name)
		info, err := os.Lstat(artifact)
		if err != nil || !info.Mode().IsRegular() {
			return errors.New("checksum artifact must be a regular file")
		}
		file, err := os.Open(artifact)
		if err != nil {
			return err
		}
		hash := sha256.New()
		_, err = io.Copy(hash, file)
		_ = file.Close()
		if err != nil {
			return err
		}
		if hex.EncodeToString(hash.Sum(nil)) != fields[0] {
			return errors.New("checksum mismatch")
		}
		entries[name] = artifact
	}
	found := map[string]bool{}
	version, native, manifestPath := "", "", ""
	zipCount := 0
	archiveName := regexp.MustCompile(`^terraform-provider-catalyst_(.+)_(linux|darwin|windows|freebsd)_(amd64|arm64|386|arm|s390x)\.zip$`)
	for name, artifact := range entries {
		if strings.HasSuffix(name, "_manifest.json") {
			if manifestPath != "" {
				return errors.New("expected one checksummed Registry manifest")
			}
			manifestPath = artifact
			continue
		}
		if !strings.HasSuffix(name, ".zip") {
			continue
		}
		zipCount++
		match := archiveName.FindStringSubmatch(name)
		if match == nil || !exactVersion.MatchString(match[1]) {
			return errors.New("unexpected archive name or non-lowercase semantic version")
		}
		if version != "" && version != match[1] {
			return errors.New("archives contain mixed release versions")
		}
		version = match[1]
		target := match[2] + "_" + match[3]
		if found[target] {
			return errors.New("duplicate platform archive")
		}
		found[target] = true
		binary := "terraform-provider-catalyst_v" + version
		if match[2] == "windows" {
			binary += ".exe"
		}
		if err := verifyZip(artifact, binary); err != nil {
			return err
		}
		if target == t.Native {
			native = artifact
		}
	}
	if len(found) != len(targets) {
		return errors.New("platform coverage mismatch")
	}
	for _, target := range targets {
		if !found[target] {
			return errors.New("platform coverage mismatch")
		}
	}
	archives, err := filepath.Glob(filepath.Join(dir, "*.zip"))
	if err != nil || len(archives) != zipCount {
		return errors.New("an archive is missing from the checksum file")
	}
	if expectedVersion != "" && version != expectedVersion {
		return errors.New("release version does not match requested version")
	}
	prefix := "terraform-provider-catalyst_" + version
	if filepath.Base(sums[0]) != prefix+"_SHA256SUMS" || filepath.Base(manifestPath) != prefix+"_manifest.json" {
		return errors.New("manifest/checksum filenames do not match archive version")
	}
	raw, err = os.ReadFile(manifestPath)
	if err != nil {
		return err
	}
	var manifest struct {
		Version  int `json:"version"`
		Metadata struct {
			Protocols []string `json:"protocol_versions"`
		} `json:"metadata"`
	}
	if json.Unmarshal(raw, &manifest) != nil || manifest.Version != 1 || !slices.Equal(manifest.Metadata.Protocols, []string{"6.0"}) {
		return errors.New("expected Terraform protocol 6.0 manifest")
	}
	if native == "" {
		return errors.New("no native archive available")
	}
	root, err := os.MkdirTemp("", "catalyst-release-check-")
	if err != nil {
		return err
	}
	defer os.RemoveAll(root)
	mirror := filepath.Join(root, "mirror")
	providerDir := filepath.Join(mirror, "registry.terraform.io", "edson-edwin", "catalyst")
	if err = os.MkdirAll(providerDir, 0700); err != nil {
		return err
	}
	if err = copyFile(native, filepath.Join(providerDir, filepath.Base(native))); err != nil {
		return err
	}
	return t.install(ctx, root, version, mirror, false)
}
func verifyZip(archivePath, binary string) error {
	archive, err := zip.OpenReader(archivePath)
	if err != nil {
		return errors.New("invalid release archive")
	}
	defer archive.Close()
	found := 0
	seen := map[string]bool{}
	var expanded uint64
	for _, file := range archive.File {
		if file.Name == "." || file.Name == ".." || seen[file.Name] || strings.ContainsAny(file.Name, "\\:") || strings.HasPrefix(file.Name, "/") || path.Clean(file.Name) != strings.TrimSuffix(file.Name, "/") || strings.HasPrefix(path.Clean(file.Name), "../") || file.Mode()&os.ModeSymlink != 0 || (!file.Mode().IsRegular() && !file.FileInfo().IsDir()) {
			return errors.New("unsafe or duplicate release archive entry")
		}
		seen[file.Name] = true
		if strings.HasPrefix(file.Name, "terraform-provider-catalyst") && file.Name != binary {
			return errors.New("unexpected additional provider executable")
		}
		if file.Name == binary {
			found++
			if file.UncompressedSize64 == 0 {
				return errors.New("empty provider executable")
			}
		}
		if file.UncompressedSize64 > 512_000_000-expanded {
			return errors.New("archive exceeds verification limit")
		}
		expanded += file.UncompressedSize64
		reader, err := file.Open()
		if err != nil {
			return err
		}
		size, readErr := io.Copy(io.Discard, io.LimitReader(reader, int64(file.UncompressedSize64)+1))
		_ = reader.Close()
		if readErr != nil || uint64(size) != file.UncompressedSize64 {
			return errors.New("corrupt archive entry")
		}
	}
	if found != 1 {
		return errors.New("expected exactly one provider executable")
	}
	return nil
}
