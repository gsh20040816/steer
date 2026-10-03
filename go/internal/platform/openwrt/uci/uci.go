// SPDX-License-Identifier: GPL-3.0-or-later

// Package uci parses the deliberately small, strict subset of UCI used by
// Steer. The OpenWrt adapter owns this representation; the semantic core never
// depends on UCI section shapes.
// Package uci parses the strict OpenWrt UCI text accepted by Steer.
package uci

import (
	"bufio"
	"errors"
	"fmt"
	"io"
	"regexp"
	"strings"
	"unicode"
)

type Section struct {
	Type    string
	ID      string
	Options map[string]string
	Lists   map[string][]string
	Line    int
}

type Document struct {
	Sections []Section
}

var identifierPattern = regexp.MustCompile(`^[a-z][a-z0-9_]{0,31}$`)
var errUnterminatedValue = errors.New("unterminated quoted or escaped value")

// IsIdentifier reports whether a section ID can be addressed safely through
// the OpenWrt uci command without changing Steer's canonical JSON ID grammar.
func IsIdentifier(value string) bool { return identifierPattern.MatchString(value) }

func Parse(r io.Reader) (Document, error) {
	var document Document
	var current *Section
	scanner := bufio.NewScanner(r)
	lineNumber, statementLine := 0, 0
	var statement strings.Builder
	process := func(tokens []string, sourceLine int) error {
		if len(tokens) == 0 {
			return nil
		}
		switch tokens[0] {
		case "config":
			if len(tokens) != 3 || tokens[1] == "" || tokens[2] == "" {
				return fmt.Errorf("UCI line %d: config requires a type and an explicit section ID", sourceLine)
			}
			if !IsIdentifier(tokens[2]) {
				return fmt.Errorf("UCI line %d: invalid Steer section ID %q", sourceLine, tokens[2])
			}
			document.Sections = append(document.Sections, Section{
				Type: tokens[1], ID: tokens[2], Line: sourceLine,
				Options: make(map[string]string), Lists: make(map[string][]string),
			})
			current = &document.Sections[len(document.Sections)-1]
		case "option", "list":
			if current == nil {
				return fmt.Errorf("UCI line %d: %s appears before config", sourceLine, tokens[0])
			}
			if len(tokens) != 3 || tokens[1] == "" {
				return fmt.Errorf("UCI line %d: %s requires a key and value", sourceLine, tokens[0])
			}
			key, value := tokens[1], tokens[2]
			if tokens[0] == "option" {
				if _, exists := current.Options[key]; exists {
					return fmt.Errorf("UCI line %d: duplicate option %q in %s %q", sourceLine, key, current.Type, current.ID)
				}
				if _, exists := current.Lists[key]; exists {
					return fmt.Errorf("UCI line %d: %q cannot be both option and list", sourceLine, key)
				}
				current.Options[key] = value
			} else {
				if _, exists := current.Options[key]; exists {
					return fmt.Errorf("UCI line %d: %q cannot be both option and list", sourceLine, key)
				}
				current.Lists[key] = append(current.Lists[key], value)
			}
		default:
			return fmt.Errorf("UCI line %d: unsupported directive %q", sourceLine, tokens[0])
		}
		return nil
	}
	for scanner.Scan() {
		lineNumber++
		if statement.Len() == 0 {
			statementLine = lineNumber
		} else {
			statement.WriteByte('\n')
		}
		statement.WriteString(scanner.Text())
		tokens, err := splitLine(statement.String())
		if errors.Is(err, errUnterminatedValue) {
			continue
		}
		if err != nil {
			return Document{}, fmt.Errorf("UCI line %d: %w", statementLine, err)
		}
		if err := process(tokens, statementLine); err != nil {
			return Document{}, err
		}
		statement.Reset()
	}
	if err := scanner.Err(); err != nil {
		return Document{}, fmt.Errorf("read UCI: %w", err)
	}
	if statement.Len() > 0 {
		return Document{}, fmt.Errorf("UCI line %d: %w", statementLine, errUnterminatedValue)
	}
	return document, nil
}

func splitLine(line string) ([]string, error) {
	var tokens []string
	var token strings.Builder
	inSingle, inDouble, escaped, started := false, false, false, false
	flush := func() {
		if started {
			tokens = append(tokens, token.String())
			token.Reset()
			started = false
		}
	}
	for _, r := range line {
		if escaped {
			token.WriteRune(r)
			started, escaped = true, false
			continue
		}
		if inSingle {
			if r == '\'' {
				inSingle = false
			} else {
				token.WriteRune(r)
			}
			started = true
			continue
		}
		if inDouble {
			switch r {
			case '"':
				inDouble = false
			case '\\':
				escaped = true
			default:
				token.WriteRune(r)
			}
			started = true
			continue
		}
		switch {
		case r == '#':
			flush()
			return tokens, nil
		case unicode.IsSpace(r):
			flush()
		case r == '\'':
			inSingle, started = true, true
		case r == '"':
			inDouble, started = true, true
		case r == '\\':
			escaped, started = true, true
		default:
			token.WriteRune(r)
			started = true
		}
	}
	if escaped || inSingle || inDouble {
		return nil, errUnterminatedValue
	}
	flush()
	return tokens, nil
}

// SetEnabled changes only the saved main switch. It never loads UCI delta paths,
// and preserves all other source bytes, including comments and multiline keys.
func SetEnabled(content string, enabled bool) (string, error) {
	if _, err := Parse(strings.NewReader(content)); err != nil {
		return "", err
	}
	replacement := "\toption enabled '0'\n"
	if enabled {
		replacement = "\toption enabled '1'\n"
	}
	offset, start, insertion := 0, 0, -1
	inMain := false
	var statement strings.Builder
	for _, line := range strings.SplitAfter(content, "\n") {
		if statement.Len() == 0 {
			start = offset
		}
		statement.WriteString(line)
		offset += len(line)
		tokens, err := splitLine(statement.String())
		if errors.Is(err, errUnterminatedValue) {
			continue
		}
		if err != nil {
			return "", err
		}
		if len(tokens) > 0 && tokens[0] == "config" {
			inMain = len(tokens) == 3 && tokens[1] == "steer" && tokens[2] == "main"
			if inMain {
				insertion = offset
			}
		}
		if inMain && len(tokens) == 3 && tokens[0] == "option" && tokens[1] == "enabled" {
			return content[:start] + replacement + content[offset:], nil
		}
		statement.Reset()
	}
	if insertion < 0 {
		return "", errors.New("saved Steer main section is missing")
	}
	if insertion > 0 && content[insertion-1] != '\n' {
		replacement = "\n" + replacement
	}
	return content[:insertion] + replacement + content[insertion:], nil
}
