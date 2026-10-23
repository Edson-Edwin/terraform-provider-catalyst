// SPDX-License-Identifier: MPL-2.0
package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"github.com/Edson-Edwin/terraform-provider-catalyst/internal/releasetools"
	"os"
	"path/filepath"
)

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, "Provider tooling failed:", err)
		os.Exit(1)
	}
}
func run() error {
	root, err := os.Getwd()
	if err != nil {
		return err
	}
	for {
		if _, err := os.Stat(filepath.Join(root, "coverage", "capabilities.json")); err == nil {
			break
		}
		parent := filepath.Dir(root)
		if parent == root {
			return errors.New("run from the provider repository")
		}
		root = parent
	}
	if len(os.Args) < 2 {
		return errors.New("use coverage-status, verify-release, verify-registry or release-readiness")
	}
	tool := releasetools.New(root)
	ctx := context.Background()
	switch os.Args[1] {
	case "coverage-status":
		return tool.Coverage(os.Stdout)
	case "verify-registry":
		if len(os.Args) != 3 {
			return errors.New("verify-registry requires an exact version")
		}
		if err = tool.VerifyRegistry(ctx, os.Args[2]); err != nil {
			return err
		}
		fmt.Println("Public Registry installation and schema verified; no Catalyst resources created.")
	case "verify-release":
		flags := flag.NewFlagSet("verify-release", flag.ContinueOnError)
		snapshot := flags.Bool("snapshot", false, "stage the local snapshot manifest")
		version := flags.String("version", "", "require exact version without v")
		fingerprint := flags.String("signer-fingerprint", "", "require trusted full GPG fingerprint")
		if err = flags.Parse(os.Args[2:]); err != nil {
			return err
		}
		if flags.NArg() != 1 {
			return errors.New("verify-release requires one artifact directory after flags")
		}
		dir := flags.Arg(0)
		if *snapshot {
			if err = tool.StageSnapshot(dir); err != nil {
				return err
			}
		}
		if err = tool.VerifyRelease(ctx, dir, *version, *fingerprint); err != nil {
			return err
		}
		fmt.Println("Checksums, manifest, 14 platform archives, mirror installation and native Terraform handshake verified.")
	case "release-readiness":
		flags := flag.NewFlagSet("release-readiness", flag.ContinueOnError)
		repo := flags.String("repo", "Edson-Edwin/terraform-provider-catalyst", "GitHub repository")
		if err = flags.Parse(os.Args[2:]); err != nil {
			return err
		}
		report, err := tool.Readiness(ctx, *repo)
		if err != nil {
			return err
		}
		encoder := json.NewEncoder(os.Stdout)
		encoder.SetIndent("", "  ")
		if err = encoder.Encode(report); err != nil {
			return err
		}
		if !report.Passed {
			return errors.New("one or more release gates remain unproven")
		}
	default:
		return errors.New("unknown provider tooling command")
	}
	return nil
}
