package client

import (
	"errors"
	"maps"
	"reflect"
	"slices"
	"time"
)

// ErrResolvedOptionsChanged means configuration returned by Resolve was edited.
// Start from fresh DialOptions to resolve a different target or policy.
var ErrResolvedOptionsChanged = errors.New("ssh client: resolved configuration changed; use fresh DialOptions")

type resolvedConfig struct {
	inputs                    resolvedInputs
	only, batch, hash         bool
	strict, alias, writeTrust string
	known                     []string
	pubkey, password          bool
	defaults                  bool
}

// resolvedInputs seals the configuration that produced the resolved policy.
// Credentials and callbacks are per-attempt inputs, not compiled configuration.
// This snapshot contains no passwords, passphrases or authentication callbacks.
type resolvedInputs struct {
	Host, User, Port string
	Sources          Sources
	IdentityFiles    []string
	SendEnvironment  []EnvironmentEntry
	Timeout          time.Duration
}

func configurationInputs(o DialOptions) resolvedInputs {
	return resolvedInputs{o.Host, o.User, o.Port, o.Sources, o.IdentityFiles, o.SendEnvironment, o.Timeout}
}

func cloneSources(s Sources) Sources {
	s.ConfigFiles = slices.Clone(s.ConfigFiles)
	s.KnownHostsFiles = slices.Clone(s.KnownHostsFiles)
	s.Environment = maps.Clone(s.Environment)
	return s
}

func sealConfiguration(o *DialOptions) {
	// Detach returned sources from the caller's original input, then retain a
	// separate private copy so edits through any exported slice/map are detected.
	o.Sources = cloneSources(o.Sources)
	inputs := configurationInputs(*o)
	inputs.Sources = cloneSources(inputs.Sources)
	inputs.IdentityFiles = slices.Clone(inputs.IdentityFiles)
	inputs.SendEnvironment = slices.Clone(inputs.SendEnvironment)
	o.resolved.inputs = inputs
}

func (r *resolvedConfig) matches(o DialOptions) bool {
	// DeepEqual also preserves nil versus empty trust-source semantics.
	return reflect.DeepEqual(r.inputs, configurationInputs(o))
}
