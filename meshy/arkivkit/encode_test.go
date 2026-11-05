package arkivkit

import (
	"encoding/hex"
	"errors"
	"os/exec"
	"strings"
	"testing"
)

func TestSelectorMatchesDocs(t *testing.T) {
	if got := hex.EncodeToString(executeSelector); got != "49650044" {
		t.Fatalf("selector = %s, docs say 49650044", got)
	}
}

func TestEncodeSortsAndValidates(t *testing.T) {
	c := Create{MinLifetime: 100, Attrs: []Attr{Str("zeta", "z", true), U64("alpha", 7, true), Schema("meshy.blocked")}}
	if _, err := c.Encode(); err != nil {
		t.Fatal(err)
	}
	bad := []Create{
		{Attrs: []Attr{Str("a", "1", true), Str("a", "2", true)}},
		{Attrs: []Attr{Str("$owner", "x", true)}},
		{Attrs: []Attr{Str("p", strings.Repeat("x", 129), true)}},
		{Attrs: []Attr{{Name: "b", Type: TBytes | Indexed, Value: []byte{1}}}},
	}
	for i, b := range bad {
		if _, err := b.Encode(); !errors.Is(err, ErrAttrs) {
			t.Errorf("case %d: err = %v", i, err)
		}
	}
}

// Cross-check our ABI encoding against Foundry's `cast abi-encode`.
func TestEncodeMatchesCast(t *testing.T) {
	if _, err := exec.LookPath("cast"); err != nil {
		t.Skip("cast not installed")
	}
	c := Create{MinLifetime: 1296000, Flags: FlagReadOnly, Attrs: []Attr{Schema("meshy.blocked"), U64("ts", 1730851200, true)}}
	got, err := c.Encode()
	if err != nil {
		t.Fatal(err)
	}
	name := func(s string) string { b := make([]byte, 32); copy(b, s); return "0x" + hex.EncodeToString(b) }
	attrs := "[(" + name("arkiv.schema") + ",136,0x" + hex.EncodeToString([]byte("meshy.blocked")) + "),(" +
		name("ts") + ",131,0x" + hex.EncodeToString(U64("ts", 1730851200, true).Value) + ")]"
	out, err := exec.Command("cast", "abi-encode", "f((uint128,uint64,uint64,uint8,(bytes32,uint8,bytes)[]))", "(0,0,1296000,1,"+attrs+")").Output()
	if err != nil {
		t.Fatal(err)
	}
	if want := strings.TrimSpace(string(out)); "0x"+hex.EncodeToString(got) != want {
		t.Fatalf("encoding mismatch\n got %x\nwant %s", got, want)
	}
}
