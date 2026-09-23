// SPDX-FileCopyrightText: 2026 Sebastien Rousseau <sebastian.rousseau@gmail.com>
// SPDX-License-Identifier: Apache-2.0

// Package attestation forwards to github.com/sebastienrousseau/scout-reporting/attestation.
//
// The verifier moved to its own module because importing a package from
// scout's module records every one of scout's dependencies in a consumer's
// go.sum, terminal UI included. That is not what a gateway signs up for when
// it adds a verifier. Everything here is an alias for the same name there,
// so an existing importer keeps compiling and accepts exactly the same
// statements; new code should import the module directly.
//
// Deprecated: import github.com/sebastienrousseau/scout-reporting/attestation.
package attestation

import "github.com/sebastienrousseau/scout-reporting/attestation"

// The format identifiers.
const (
	StatementType         = attestation.StatementType
	PredicateType         = attestation.PredicateType
	SubjectKindDescriptor = attestation.SubjectKindDescriptor
)

// Statement is attestation.Statement.
type Statement = attestation.Statement

// Subject is attestation.Subject.
type Subject = attestation.Subject

// Evaluation is attestation.Evaluation.
type Evaluation = attestation.Evaluation

// Plan is attestation.Plan.
type Plan = attestation.Plan

// Target is attestation.Target.
type Target = attestation.Target

// ServerIdentity is attestation.ServerIdentity.
type ServerIdentity = attestation.ServerIdentity

// Basis is attestation.Basis.
type Basis = attestation.Basis

// Instrument is attestation.Instrument.
type Instrument = attestation.Instrument

// Verdict is attestation.Verdict.
type Verdict = attestation.Verdict

// Counts is attestation.Counts.
type Counts = attestation.Counts

// Score is attestation.Score.
type Score = attestation.Score

// Change is attestation.Change.
type Change = attestation.Change

// Delta is attestation.Delta.
type Delta = attestation.Delta

// ErrNoSuchCheck is attestation.ErrNoSuchCheck.
var ErrNoSuchCheck = attestation.ErrNoSuchCheck

// The functions.
var (
	Parse      = attestation.Parse
	SubjectFor = attestation.SubjectFor
	Compare    = attestation.Compare
)
