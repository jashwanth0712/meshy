package policy

import (
	"errors"
	"strings"
	"testing"
)

const grant = "claude-1.db1.prod.acme.eth"

func sample() *Policy {
	return &Policy{
		V: Version, Grant: grant, GrantTokenID: "42",
		Paths: []PathRule{{"/srv/reports", ReadOnly}, {"/srv/tmp/", ReadWrite}, {"/srv/reports", ReadWrite}},
	}
}

func TestValidateNormalizes(t *testing.T) {
	p := sample()
	if err := p.Validate(); err != nil {
		t.Fatal(err)
	}
	if got := p.AllowedSummary(); got != "[/srv/reports:rw,/srv/tmp:rw]" {
		t.Fatalf("summary = %s", got)
	}
}

func TestValidateRejects(t *testing.T) {
	cases := map[string]struct {
		p   Policy
		err error
	}{
		"relative": {Policy{V: 1, Grant: grant, Paths: []PathRule{{"srv", ReadOnly}}}, ErrRelativePath},
		"root":     {Policy{V: 1, Grant: grant, Paths: []PathRule{{"/srv/..", ReadOnly}}}, ErrRootPath},
		"mode":     {Policy{V: 1, Grant: grant, Paths: []PathRule{{"/srv", "x"}}}, ErrBadMode},
		"empty":    {Policy{V: 1, Grant: grant}, ErrNoPaths},
		"version":  {Policy{V: 9, Grant: grant, Paths: []PathRule{{"/srv", ReadOnly}}}, ErrVersion},
		"nogrant":  {Policy{V: 1, Paths: []PathRule{{"/srv", ReadOnly}}}, ErrNoGrant},
	}
	for name, c := range cases {
		if err := c.p.Validate(); !errors.Is(err, c.err) {
			t.Errorf("%s: err = %v, want %v", name, err, c.err)
		}
	}
}

func TestCheck(t *testing.T) {
	p := &Policy{V: 1, Grant: grant, Paths: []PathRule{{"/srv/reports", ReadOnly}, {"/srv/reports/drafts", ReadWrite}}}
	if err := p.Validate(); err != nil {
		t.Fatal(err)
	}
	cases := []struct {
		path  string
		write bool
		ok    bool
	}{
		{"/srv/reports", false, true},
		{"/srv/reports/q3.csv", false, true},
		{"/srv/reports/q3.csv", true, false},       // ro
		{"/srv/reports/drafts/a.md", true, true},   // longest prefix rw
		{"/srv/reportsX/a", false, false},          // component boundary
		{"/srv/reports/../secrets", false, false},  // cleaned before matching
		{"/etc/shadow", false, false},
		{"/srv", false, false},
	}
	for _, c := range cases {
		if d := p.Check(c.path, c.write); d.Allowed != c.ok {
			t.Errorf("Check(%q, write=%v) = %+v, want allowed=%v", c.path, c.write, d, c.ok)
		}
	}
}

func TestCommands(t *testing.T) {
	p := &Policy{Cmds: []string{"cat", "ls"}}
	if !p.AllowsCommand("/bin/cat") || p.AllowsCommand("rm") {
		t.Fatal("command allow-list broken")
	}
	if !(&Policy{}).AllowsCommand("anything") {
		t.Fatal("empty list should allow")
	}
}

func TestEnvelopeRoundTrip(t *testing.T) {
	daemon, _ := GenerateKey()
	org, _ := GenerateKey()
	other, _ := GenerateKey()

	rec, err := Seal(sample(), daemon.PublicKey(), org.PublicKey())
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(rec, "/srv/reports") {
		t.Fatal("plaintext path leaked into record")
	}
	p, err := Open(rec, grant, daemon)
	if err != nil || p.GrantTokenID != "42" {
		t.Fatalf("daemon open: %v %+v", err, p)
	}
	if _, err := Open(rec, grant, org); err != nil {
		t.Fatalf("org open: %v", err)
	}
	if _, err := Open(rec, grant, other); !errors.Is(err, ErrNotRecipient) {
		t.Fatalf("stranger open err = %v", err)
	}
	// Record copied onto another grant name must not decrypt.
	if _, err := Open(rec, "evil.db1.prod.acme.eth", daemon); err == nil {
		t.Fatal("record replayed onto another grant decrypted")
	}
}

func TestKeyEncoding(t *testing.T) {
	k, _ := GenerateKey()
	pub, err := ParsePublicKey(EncodePublicKey(k.PublicKey()))
	if err != nil || !pub.Equal(k.PublicKey()) {
		t.Fatal("public key round trip")
	}
	priv, err := ParsePrivateKey(EncodePrivateKey(k))
	if err != nil || !priv.Equal(k) {
		t.Fatal("private key round trip")
	}
}
