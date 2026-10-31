// SPDX-License-Identifier: GPL-3.0-or-later

// Package apply coordinates the synchronous cross-platform Apply lifecycle.
// Backends own platform resources and service operations. Callers validate
// the Intent with their platform's validation before calling Run.
package apply

import (
	"context"

	"github.com/gsh20040816/steer/go/internal/compiler"
	"github.com/gsh20040816/steer/go/internal/generation"
	model "github.com/gsh20040816/steer/go/internal/intent"
)

type Result struct {
	OK                  bool              `json:"ok"`
	Error               string            `json:"error,omitempty"`
	CandidateGeneration string            `json:"candidate_generation,omitempty"`
	Generation          string            `json:"generation,omitempty"`
	Activated           bool              `json:"activated"`
	Restarted           bool              `json:"restarted"`
	IntentDigest        string            `json:"intent_digest,omitempty"`
	RuntimeDigest       string            `json:"runtime_digest,omitempty"`
	Validation          *model.Validation `json:"validation,omitempty"`
}

type Record struct {
	Sequence  string `json:"sequence"`
	Timestamp string `json:"timestamp,omitempty"`
	Result    Result `json:"result"`
}

// Backend is one platform adapter. Activate reports whether it restarted the
// service: an unchanged, healthy runtime only switches current to the
// candidate (see generation.Unchanged), everything else stops and starts.
type Backend interface {
	Prepare(context.Context, model.Intent, compiler.Output) (generation.Candidate, error)
	Activate(context.Context, generation.Candidate) (bool, error)
	Healthy(context.Context, generation.Candidate) error
	Finalize(context.Context, generation.Candidate) error
	Disable(context.Context) error
}

func Run(ctx context.Context, value model.Intent, options compiler.Options, backend Backend) (Result, error) {
	compiled := compiler.Compile(value, options)
	result := Result{IntentDigest: compiled.IntentDigest, RuntimeDigest: compiled.RuntimeDigest}
	if !value.Main.Enabled {
		if err := backend.Disable(ctx); err != nil {
			return result, err
		}
		result.OK = true
		return result, nil
	}
	candidate, err := backend.Prepare(ctx, value, compiled)
	if err != nil {
		return result, err
	}
	result.CandidateGeneration = candidate.Directory
	restarted, err := backend.Activate(ctx, candidate)
	result.Restarted = restarted
	if err != nil {
		return result, err
	}
	result.Generation = candidate.Directory
	result.Activated = true
	if err := backend.Healthy(ctx, candidate); err != nil {
		return result, err
	}
	if err := backend.Finalize(ctx, candidate); err != nil {
		return result, err
	}
	result.OK = true
	return result, nil
}
