// SPDX-License-Identifier: GPL-3.0-or-later

// Command steer-geodata-build creates and verifies the package-owned SRS tree
// published by CI. It is a build tool and is never installed on user devices.
package main

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"sync"

	"github.com/gsh20040816/steer/go/internal/geodata"
	"github.com/snowie2000/geoview/geoip"
	"github.com/snowie2000/geoview/geosite"
	"github.com/snowie2000/geoview/global"
	"github.com/snowie2000/geoview/protohelper"
)

type buildTask struct {
	Kind       string
	Category   string
	Attributes []string
	Optional   bool
}

func main() {
	if err := run(os.Args[1:]); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func run(args []string) error {
	if len(args) == 0 {
		return errors.New("usage: steer-geodata-build generate|verify [flags]")
	}
	switch args[0] {
	case "generate":
		return runGenerate(args[1:])
	case "verify":
		return runVerify(args[1:])
	default:
		return errors.New("usage: steer-geodata-build generate|verify [flags]")
	}
}

func runGenerate(args []string) error {
	flags := flag.NewFlagSet("generate", flag.ContinueOnError)
	geoSitePath := flags.String("geosite", "", "Loyalsoldier geosite.dat")
	geoIPPath := flags.String("geoip", "", "Loyalsoldier geoip.dat")
	singBoxBinary := flags.String("sing-box", "sing-box", "sing-box compiler")
	upstreamVersion := flags.String("upstream-version", "", "Loyalsoldier release tag")
	outputDirectory := flags.String("output", "", "new output directory")
	workers := flags.Int("workers", runtime.NumCPU(), "parallel conversion workers")
	if err := flags.Parse(args); err != nil {
		return err
	}
	if flags.NArg() != 0 {
		return errors.New("generate accepts flags only")
	}
	if *geoSitePath == "" || *geoIPPath == "" || *upstreamVersion == "" || *outputDirectory == "" {
		return errors.New("generate requires --geosite, --geoip, --upstream-version and --output")
	}
	if *workers < 1 {
		return errors.New("workers must be positive")
	}
	if entries, err := os.ReadDir(*outputDirectory); err == nil && len(entries) > 0 {
		return fmt.Errorf("output directory %q is not empty", *outputDirectory)
	} else if err != nil && !os.IsNotExist(err) {
		return err
	}
	if err := os.MkdirAll(filepath.Join(*outputDirectory, "rules"), 0o755); err != nil {
		return err
	}
	temporaryDirectory, err := os.MkdirTemp("", "steer-geodata-build.")
	if err != nil {
		return err
	}
	defer os.RemoveAll(temporaryDirectory)

	versionOutput, err := exec.Command(*singBoxBinary, "version").Output()
	if err != nil {
		return fmt.Errorf("inspect sing-box compiler: %w", err)
	}
	singBoxVersion, err := parseSingBoxVersion(string(versionOutput))
	if err != nil {
		return err
	}
	tasks, err := enumerateTasks(*geoSitePath, *geoIPPath)
	if err != nil {
		return err
	}
	global.Lowmem = true
	rules, err := buildRules(tasks, *geoSitePath, *geoIPPath, *singBoxBinary, *outputDirectory, temporaryDirectory, *workers)
	if err != nil {
		return err
	}
	geoSiteDigest, err := fileDigest(*geoSitePath)
	if err != nil {
		return err
	}
	geoIPDigest, err := fileDigest(*geoIPPath)
	if err != nil {
		return err
	}
	sort.Slice(rules, func(i, j int) bool { return rules[i].Tag < rules[j].Tag })
	manifest := geodata.Manifest{
		SchemaVersion: geodata.ManifestSchemaVersion,
		Upstream: geodata.UpstreamIdentity{
			Repository: geodata.UpstreamRepository, Version: *upstreamVersion,
			GeoSiteSHA256: geoSiteDigest, GeoIPSHA256: geoIPDigest,
		},
		Tools: geodata.ToolIdentity{GeoViewRef: geodata.GeoViewCommit, SingBoxVersion: singBoxVersion},
		Rules: rules,
	}
	if err := geodata.ValidateManifest(manifest); err != nil {
		return err
	}
	manifestPath := filepath.Join(*outputDirectory, "manifest.json")
	file, err := os.OpenFile(manifestPath, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o644)
	if err != nil {
		return err
	}
	encoder := json.NewEncoder(file)
	encoder.SetEscapeHTML(false)
	encoder.SetIndent("", "  ")
	encodeErr := encoder.Encode(manifest)
	closeErr := file.Close()
	if encodeErr != nil {
		return encodeErr
	}
	if closeErr != nil {
		return closeErr
	}
	return geodata.VerifyDirectory(*outputDirectory)
}

func runVerify(args []string) error {
	flags := flag.NewFlagSet("verify", flag.ContinueOnError)
	directory := flags.String("directory", "", "extracted seed directory")
	if err := flags.Parse(args); err != nil {
		return err
	}
	if flags.NArg() != 0 || *directory == "" {
		return errors.New("verify requires --directory")
	}
	return geodata.VerifyDirectory(*directory)
}

func enumerateTasks(geoSitePath, geoIPPath string) ([]buildTask, error) {
	site, err := geosite.LoadV2SiteFromFile(geoSitePath)
	if err != nil {
		return nil, fmt.Errorf("read GeoSite codes: %w", err)
	}
	defer site.Close()
	codes := site.Codes()
	sort.Strings(codes)
	tasks := make([]buildTask, 0, len(codes))
	for _, code := range codes {
		category := strings.ToLower(code)
		tasks = append(tasks, buildTask{Kind: "geosite", Category: category})
		entries, err := site.ReadSites([]string{code}, true)
		if err != nil {
			return nil, fmt.Errorf("read GeoSite category %s: %w", code, err)
		}
		if len(entries) != 1 {
			return nil, fmt.Errorf("read GeoSite category %s: got %d entries", code, len(entries))
		}
		attributes := map[string]struct{}{}
		for _, domain := range entries[0].Domain {
			for _, attribute := range domain.Attribute {
				if attribute.Key != "" {
					attributes[strings.ToLower(attribute.Key)] = struct{}{}
				}
			}
		}
		attributeNames := make([]string, 0, len(attributes))
		for attribute := range attributes {
			attributeNames = append(attributeNames, attribute)
		}
		sort.Strings(attributeNames)
		for _, attribute := range attributeNames {
			tasks = append(tasks, buildTask{
				Kind: "geosite", Category: category + "@" + attribute,
				Attributes: []string{attribute}, Optional: true,
			})
		}
	}
	file, err := os.Open(geoIPPath)
	if err != nil {
		return nil, err
	}
	geoIPCodes := protohelper.CodeListByReader(file)
	file.Close()
	names := make([]string, 0, len(geoIPCodes))
	for name := range geoIPCodes {
		names = append(names, strings.ToLower(name))
	}
	sort.Strings(names)
	for _, name := range names {
		tasks = append(tasks, buildTask{Kind: "geoip", Category: name})
	}
	return tasks, nil
}

func buildRules(tasks []buildTask, geoSitePath, geoIPPath, singBoxBinary, outputDirectory, temporaryDirectory string, workers int) ([]geodata.Rule, error) {
	jobs := make(chan buildTask)
	type result struct {
		rule *geodata.Rule
		err  error
	}
	results := make(chan result)
	var group sync.WaitGroup
	for range workers {
		group.Add(1)
		go func() {
			defer group.Done()
			for task := range jobs {
				rule, err := buildRule(task, geoSitePath, geoIPPath, singBoxBinary, outputDirectory, temporaryDirectory)
				results <- result{rule: rule, err: err}
			}
		}()
	}
	go func() {
		for _, task := range tasks {
			jobs <- task
		}
		close(jobs)
		group.Wait()
		close(results)
	}()
	rules := make([]geodata.Rule, 0, len(tasks))
	var firstErr error
	for item := range results {
		if item.err != nil && firstErr == nil {
			firstErr = item.err
		}
		if item.err == nil && item.rule != nil {
			rules = append(rules, *item.rule)
		}
	}
	if firstErr != nil {
		return nil, firstErr
	}
	return rules, nil
}

func buildRule(task buildTask, geoSitePath, geoIPPath, singBoxBinary, outputDirectory, temporaryDirectory string) (*geodata.Rule, error) {
	tag := "steer-" + task.Kind + "-" + task.Category
	var source any
	var err error
	switch task.Kind {
	case "geosite":
		base := strings.SplitN(task.Category, "@", 2)[0]
		source, err = geosite.NewGeositeHandler(geoSitePath, true, true).ToRuleSet(
			map[string][]string{strings.ToUpper(base): task.Attributes}, false,
		)
	case "geoip":
		source, err = (&geoip.GeoIPDatIn{
			URI: geoIPPath, Want: map[string]bool{strings.ToUpper(task.Category): true}, MustExist: true,
		}).ToRuleSet(geoip.IPv4 | geoip.IPv6)
	}
	if err != nil {
		if task.Optional && err.Error() == "empty domain set" {
			return nil, nil
		}
		return nil, fmt.Errorf("convert %s:%s: %w", task.Kind, task.Category, err)
	}
	jsonPath := filepath.Join(temporaryDirectory, tag+".json")
	encoded, err := json.Marshal(source)
	if err != nil {
		return nil, err
	}
	if err := os.WriteFile(jsonPath, append(encoded, '\n'), 0o600); err != nil {
		return nil, err
	}
	relativePath := filepath.ToSlash(filepath.Join("rules", tag+".srs"))
	outputPath := filepath.Join(outputDirectory, filepath.FromSlash(relativePath))
	if output, err := exec.Command(singBoxBinary, "rule-set", "compile", "--output", outputPath, jsonPath).CombinedOutput(); err != nil {
		return nil, fmt.Errorf("compile %s:%s: %w: %s", task.Kind, task.Category, err, strings.TrimSpace(string(output)))
	}
	decodedPath := filepath.Join(temporaryDirectory, tag+".decoded.json")
	if output, err := exec.Command(singBoxBinary, "rule-set", "decompile", "--output", decodedPath, outputPath).CombinedOutput(); err != nil {
		return nil, fmt.Errorf("decompile %s:%s: %w: %s", task.Kind, task.Category, err, strings.TrimSpace(string(output)))
	}
	decoded, err := os.ReadFile(decodedPath)
	if err != nil {
		return nil, err
	}
	var check struct {
		Rules []json.RawMessage `json:"rules"`
	}
	if err := json.Unmarshal(decoded, &check); err != nil || len(check.Rules) == 0 {
		return nil, fmt.Errorf("decompiled %s:%s has no rules", task.Kind, task.Category)
	}
	info, err := os.Stat(outputPath)
	if err != nil {
		return nil, err
	}
	digest, err := fileDigest(outputPath)
	if err != nil {
		return nil, err
	}
	rule := &geodata.Rule{
		Kind: task.Kind, Category: task.Category, Tag: tag, Path: relativePath,
		SHA256: digest, Size: info.Size(),
	}
	return rule, nil
}

func fileDigest(path string) (string, error) {
	file, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer file.Close()
	digest := sha256.New()
	if _, err := io.Copy(digest, file); err != nil {
		return "", err
	}
	return hex.EncodeToString(digest.Sum(nil)), nil
}

func parseSingBoxVersion(output string) (string, error) {
	const prefix = "sing-box version "
	for _, line := range strings.Split(output, "\n") {
		if strings.HasPrefix(line, prefix) {
			version := strings.TrimSpace(strings.TrimPrefix(line, prefix))
			if version != "" {
				return version, nil
			}
		}
	}
	return "", errors.New("cannot parse sing-box compiler version")
}
