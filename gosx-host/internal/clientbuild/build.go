// Package clientbuild packages Go-WASM application code and upstream GoSX loaders.
package clientbuild

import (
	"compress/gzip"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/andybalholm/brotli"
	"m31labs.dev/gosx/buildmanifest"
)

func Build(module string) error {
	metadata := exec.Command("go", "list", "-m", "-json", "m31labs.dev/gosx")
	metadata.Dir = module
	output, err := metadata.Output()
	if err != nil {
		return fmt.Errorf("resolve pinned GoSX: %w", err)
	}
	var sdk struct{ Dir, Version string }
	if err = json.Unmarshal(output, &sdk); err != nil {
		return err
	}
	if sdk.Dir == "" || sdk.Version == "" {
		return fmt.Errorf("the editor requires a versioned GoSX dependency")
	}
	root := filepath.Join(module, "clientassets", "generated")
	old, _ := buildmanifest.Load(filepath.Join(root, "build.json"))
	// The Go tool directive shares the pinned SDK and verified go.sum with the
	// runtime assets, including when the SDK uses an untagged revision.
	command := exec.Command("go", "tool", "gosx", "build", "--go-wasm-only", "--prod", "--output", root, ".")
	command.Dir, command.Stdout, command.Stderr = module, os.Stdout, os.Stderr
	if err = command.Run(); err != nil {
		return fmt.Errorf("build Go editor runtime: %w", err)
	}
	manifest, err := buildmanifest.Load(filepath.Join(root, "build.json"))
	if err != nil {
		return err
	}
	// These are shipped by the pinned Go module. No application JavaScript is
	// authored or transpiled, and no JavaScript package manager is involved.
	for name, destination := range map[string]*buildmanifest.HashedAsset{
		"bootstrap-runtime":         &manifest.Runtime.BootstrapRuntime,
		"bootstrap-feature-engines": &manifest.Runtime.BootstrapFeatureEngines,
		"patch":                     &manifest.Runtime.Patch,
	} {
		data, err := os.ReadFile(filepath.Join(sdk.Dir, "client", "js", name+".js"))
		if err != nil {
			return err
		}
		hash := sha256.Sum256(data)
		*destination = buildmanifest.HashedAsset{File: name + "." + hex.EncodeToString(hash[:8]) + ".js", Hash: hex.EncodeToString(hash[:8]), Size: int64(len(data))}
		if err = os.WriteFile(filepath.Join(root, "assets", "runtime", destination.File), data, 0644); err != nil {
			return err
		}
	}
	manifest.Runtime.Bootstrap = manifest.Runtime.BootstrapRuntime
	manifest.SourceRoot = ""
	for bucket, assets := range assetGroups(manifest) {
		brotliQuality := 9
		if bucket == "go-wasm" {
			// Spend more build time on the largest cold-start download.
			brotliQuality = brotli.BestCompression
		}
		for _, asset := range assets {
			path := filepath.Join(root, "assets", bucket, asset.File)
			data, err := os.ReadFile(path)
			if err != nil {
				return err
			}
			out, err := os.Create(path + ".br")
			if err != nil {
				return err
			}
			br := brotli.NewWriterLevel(out, brotliQuality)
			_, err = br.Write(data)
			if e := br.Close(); err == nil {
				err = e
			}
			if e := out.Close(); err == nil {
				err = e
			}
			if err != nil {
				return err
			}
			out, err = os.Create(path + ".gz")
			if err != nil {
				return err
			}
			z, _ := gzip.NewWriterLevel(out, gzip.BestCompression)
			_, err = z.Write(data)
			if e := z.Close(); err == nil {
				err = e
			}
			if e := out.Close(); err == nil {
				err = e
			}
			if err != nil {
				return err
			}
			gzInfo, err := os.Stat(path + ".gz")
			if err != nil {
				return err
			}
			brInfo, err := os.Stat(path + ".br")
			if err != nil {
				return err
			}
			for name, candidate := range manifest.GoWASM {
				if candidate.File == asset.File {
					candidate.GzipSize, candidate.BrotliSize = gzInfo.Size(), brInfo.Size()
					manifest.GoWASM[name] = candidate
				}
			}
			for _, candidate := range []*buildmanifest.HashedAsset{&manifest.Runtime.StandardGoWASMExec, &manifest.Runtime.BootstrapRuntime, &manifest.Runtime.BootstrapFeatureEngines, &manifest.Runtime.Patch} {
				if candidate.File == asset.File {
					candidate.GzipSize, candidate.BrotliSize = gzInfo.Size(), brInfo.Size()
				}
			}
		}
	}
	manifest.Runtime.Bootstrap = manifest.Runtime.BootstrapRuntime
	license, err := os.ReadFile(filepath.Join(sdk.Dir, "LICENSE"))
	if err != nil {
		return err
	}
	if err = os.WriteFile(filepath.Join(root, "GoSX-LICENSE.txt"), license, 0644); err != nil {
		return err
	}
	encoded, err := json.MarshalIndent(manifest, "", "  ")
	if err != nil {
		return err
	}
	if err = os.WriteFile(filepath.Join(root, "build.json"), append(encoded, '\n'), 0644); err != nil {
		return err
	}
	// Remove only files named by our previous generated manifest. Stale WASM
	// modules must not accumulate in subsequent embedded executable builds.
	current := map[string]bool{}
	for bucket, assets := range assetGroups(manifest) {
		for _, asset := range assets {
			current[filepath.Join(bucket, asset.File)] = true
		}
	}
	if old != nil {
		for bucket, assets := range assetGroups(old) {
			for _, asset := range assets {
				rel := filepath.Join(bucket, asset.File)
				if !current[rel] && filepath.IsLocal(rel) && !strings.ContainsAny(asset.File, `/\`) {
					for _, suffix := range []string{"", ".gz", ".br"} {
						_ = os.Remove(filepath.Join(root, "assets", rel) + suffix)
					}
				}
			}
		}
	}
	return nil
}

func assetGroups(m *buildmanifest.Manifest) map[string][]buildmanifest.HashedAsset {
	groups := map[string][]buildmanifest.HashedAsset{"runtime": {m.Runtime.StandardGoWASMExec, m.Runtime.BootstrapRuntime, m.Runtime.BootstrapFeatureEngines, m.Runtime.Patch}}
	for _, asset := range m.GoWASM {
		groups["go-wasm"] = append(groups["go-wasm"], asset)
	}
	return groups
}
