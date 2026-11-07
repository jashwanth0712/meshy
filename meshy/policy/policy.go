// Package policy defines Meshy's per-grant access policy: which folders an
// agent may touch on a resource, and in which mode. The plaintext policy is
// stored ECIES-encrypted in the grant's ENS `meshy.policy` text record.
package policy

import (
	"encoding/json"
	"errors"
	"fmt"
	"path/filepath"
	"sort"
	"strings"
)

// Version is the current policy schema version.
const Version = 1

// Mode is the access mode for a path rule.
type Mode string

const (
	ReadOnly  Mode = "ro"
	ReadWrite Mode = "rw"
)

// PathRule grants access to one directory subtree (or a single file).
type PathRule struct {
	Path string `json:"p"`
	Mode Mode   `json:"m"`
}

// Policy is the decrypted content of a grant's `meshy.policy` record.
type Policy struct {
	V int `json:"v"`
	// Grant is the full ENS name of the grant this policy belongs to,
	// e.g. "claude-1.db1.prod.acme.eth". It is also bound as AEAD associated
	// data, so a policy cannot be copied onto another grant name.
	Grant string `json:"grant"`
	// GrantTokenID is the ENSv2 token id of the grant at issue time. Daemons
	// reject the policy if the live token id differs (the label was
	// unregistered and re-registered, and stale resolver records resurfaced).
	GrantTokenID string `json:"grantTokenId"`
	// GrantExpiry (unix seconds) is the grant's exact expiry, chosen before the
	// grant is registered, so register + policy can be one batched transaction.
	// Daemons reject the policy if the live grant's expiry differs (a
	// re-registered label never reuses a stale policy). 0: not checked.
	GrantExpiry uint64     `json:"grantExpiry,omitempty"`
	Paths       []PathRule `json:"paths"`
	// Cmds optionally restricts which executables may be started (basename
	// match). Empty means any command, still confined to Paths.
	Cmds []string `json:"cmds,omitempty"`
}

var (
	ErrNoPaths      = errors.New("policy: no paths granted")
	ErrRelativePath = errors.New("policy: path must be absolute")
	ErrRootPath     = errors.New("policy: granting / is not allowed")
	ErrBadMode      = errors.New("policy: mode must be ro or rw")
	ErrVersion      = errors.New("policy: unsupported version")
	ErrNoGrant      = errors.New("policy: grant name missing")
)

// Validate checks the policy and normalizes paths in place (cleaned,
// deduplicated, sorted; rw wins over ro for duplicates).
func (p *Policy) Validate() error {
	if p.V != Version {
		return fmt.Errorf("%w: %d", ErrVersion, p.V)
	}
	if strings.TrimSpace(p.Grant) == "" {
		return ErrNoGrant
	}
	if len(p.Paths) == 0 {
		return ErrNoPaths
	}
	merged := map[string]Mode{}
	for _, r := range p.Paths {
		if !filepath.IsAbs(r.Path) {
			return fmt.Errorf("%w: %q", ErrRelativePath, r.Path)
		}
		clean := filepath.Clean(r.Path)
		if clean == "/" {
			return ErrRootPath
		}
		if r.Mode != ReadOnly && r.Mode != ReadWrite {
			return fmt.Errorf("%w: %q", ErrBadMode, r.Mode)
		}
		if merged[clean] != ReadWrite {
			merged[clean] = r.Mode
		}
	}
	out := make([]PathRule, 0, len(merged))
	for path, m := range merged {
		out = append(out, PathRule{Path: path, Mode: m})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Path < out[j].Path })
	p.Paths = out
	return nil
}

// Decision is the result of checking one access against a policy.
type Decision struct {
	Allowed bool
	Rule    *PathRule // matching rule, if any
	Reason  string    // human/agent readable reason when denied
}

// Check decides whether an access to path (already resolved to an absolute,
// symlink-free path by the caller) is allowed. write is true for any
// mutating operation (write, create, unlink, rename, chmod, truncate).
func (p *Policy) Check(path string, write bool) Decision {
	clean := filepath.Clean(path)
	var best *PathRule
	for i := range p.Paths {
		r := &p.Paths[i]
		if within(r.Path, clean) && (best == nil || len(r.Path) > len(best.Path)) {
			best = r
		}
	}
	switch {
	case best == nil:
		return Decision{Reason: "outside grant"}
	case write && best.Mode != ReadWrite:
		return Decision{Rule: best, Reason: "read-only grant"}
	default:
		return Decision{Allowed: true, Rule: best}
	}
}

// AllowsCommand reports whether the executable may be started.
func (p *Policy) AllowsCommand(argv0 string) bool {
	if len(p.Cmds) == 0 {
		return true
	}
	base := filepath.Base(argv0)
	for _, c := range p.Cmds {
		if c == base {
			return true
		}
	}
	return false
}

// AllowedSummary renders the grant for agent-facing error messages,
// e.g. "[/srv/reports:ro,/srv/tmp:rw]".
func (p *Policy) AllowedSummary() string {
	parts := make([]string, len(p.Paths))
	for i, r := range p.Paths {
		parts[i] = r.Path + ":" + string(r.Mode)
	}
	return "[" + strings.Join(parts, ",") + "]"
}

// Marshal returns the canonical JSON encoding.
func (p *Policy) Marshal() ([]byte, error) { return json.Marshal(p) }

// Unmarshal parses and validates a plaintext policy.
func Unmarshal(b []byte) (*Policy, error) {
	var p Policy
	if err := json.Unmarshal(b, &p); err != nil {
		return nil, fmt.Errorf("policy: %w", err)
	}
	if err := p.Validate(); err != nil {
		return nil, err
	}
	return &p, nil
}

// within reports whether target equals base or is inside it, on a path
// component boundary ("/srv/rep" does not contain "/srv/reports").
func within(base, target string) bool {
	if target == base {
		return true
	}
	return strings.HasPrefix(target, base+string(filepath.Separator))
}
