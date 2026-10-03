// SPDX-License-Identifier: GPL-3.0-or-later
package uci

// Parser tests intentionally stay below the OpenWrt adapter boundary.

import (
	"strings"
	"testing"
)

func TestParseStrictDocument(t *testing.T) {
	doc, err := Parse(strings.NewReader(`
# comment
config steer 'main'
	option schema_version '4'
	list managed_zone 'lan'
	list managed_zone "guest"
`))
	if err != nil {
		t.Fatal(err)
	}
	if len(doc.Sections) != 1 || doc.Sections[0].ID != "main" {
		t.Fatalf("unexpected document: %#v", doc)
	}
	if got := doc.Sections[0].Lists["managed_zone"]; len(got) != 2 || got[1] != "guest" {
		t.Fatalf("unexpected list: %#v", got)
	}
}

func TestParseRejectsAmbiguousInput(t *testing.T) {
	for name, input := range map[string]string{
		"anonymous":  "config steer\n",
		"hyphenated": "config route 'route-a'\n",
		"uppercase":  "config route 'RouteA'\n",
		"duplicate":  "config steer 'main'\noption enabled '1'\noption enabled '0'\n",
		"mixed":      "config steer 'main'\noption zone 'lan'\nlist zone 'guest'\n",
		"unknown":    "config steer 'main'\ndelete enabled\n",
		"quote":      "config steer 'main\n",
	} {
		t.Run(name, func(t *testing.T) {
			if _, err := Parse(strings.NewReader(input)); err == nil {
				t.Fatal("expected parse failure")
			}
		})
	}
}

func TestParsePreservesMultilineSingleQuotedOption(t *testing.T) {
	doc, err := Parse(strings.NewReader("config node 'ssh_key'\n\toption private_key 'line one\nline two\n'\n"))
	if err != nil {
		t.Fatal(err)
	}
	if got := doc.Sections[0].Options["private_key"]; got != "line one\nline two\n" {
		t.Fatalf("multiline private key changed: %q", got)
	}
}

func TestSetEnabledPreservesOtherSavedBytes(t *testing.T) {
	content := "# configuration\nconfig steer 'main'\n\toption enabled '1'\n\toption log_level 'debug'\n\nconfig node 'feed_node'\n\toption enabled '1'\n\toption private_key 'line one\nline two'\n"
	disabled, err := SetEnabled(content, false)
	if err != nil {
		t.Fatal(err)
	}
	expected := strings.Replace(content, "option enabled '1'", "option enabled '0'", 1)
	if disabled != expected {
		t.Fatalf("changed unrelated saved bytes: %q", disabled)
	}
	enabled, err := SetEnabled(disabled, true)
	if err != nil || enabled != content {
		t.Fatalf("roundtrip: %q %v", enabled, err)
	}
	for _, input := range []string{"config steer 'main'", "config steer 'main'\n\nconfig node 'feed'\n\toption enabled '0'\n"} {
		updated, err := SetEnabled(input, true)
		if err != nil {
			t.Fatal(err)
		}
		parsed, err := Parse(strings.NewReader(updated))
		if err != nil || parsed.Sections[0].Options["enabled"] != "1" {
			t.Fatalf("insert missing option: %q %v", updated, err)
		}
	}
	if _, err := SetEnabled("config node 'main'\n", false); err == nil {
		t.Fatal("accepted missing main")
	}
	if _, err := SetEnabled("config steer 'main'\noption enabled 'unterminated", false); err == nil {
		t.Fatal("accepted malformed saved file")
	}
}
