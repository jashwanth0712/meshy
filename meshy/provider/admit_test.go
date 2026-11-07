package provider

import (
	"math/big"
	"testing"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/crypto"

	"github.com/jashwanth0712/meshy/meshy/enskit"
)

func TestAttestationSignRecover(t *testing.T) {
	key, _ := crypto.GenerateKey()
	dns, _ := enskit.DNSEncode("db1.prod.acme.eth")
	a := Attestation{
		ProviderID: 1, Name: "db1.prod.acme.eth", DNSName: "0x" + common.Bytes2Hex(dns),
		Resource: "12345", IssuedAt: 1000, ValidUntil: 4600,
		PeerKey:  "0x" + common.Bytes2Hex(make([]byte, 32)),
		Registry: "0x8B93557b315E825C657D48A4CB962A3fF7E4eEec", ChainID: 11155111,
	}
	if err := SignAttestation(&a, key); err != nil {
		t.Fatal(err)
	}
	sig := common.FromHex(a.Sig)
	if len(sig) != 65 || (sig[64] != 27 && sig[64] != 28) {
		t.Fatalf("sig shape: %x", sig)
	}
	var pk [32]byte
	d := AttestationDigest(a.ChainID, common.HexToAddress(a.Registry), 1, dns, big.NewInt(12345), 1000, 4600, pk)
	sig[64] -= 27
	pub, err := crypto.SigToPub(d.Bytes(), sig)
	if err != nil || crypto.PubkeyToAddress(*pub) != crypto.PubkeyToAddress(key.PublicKey) {
		t.Fatal("recovered signer mismatch")
	}
	// Any field change must change the digest.
	if AttestationDigest(a.ChainID, common.HexToAddress(a.Registry), 1, dns, big.NewInt(12346), 1000, 4600, pk) == d {
		t.Fatal("digest ignores resource")
	}
}

func TestOrgFromName(t *testing.T) {
	for in, want := range map[string]string{
		"claude-1.agents.acme.eth": "acme.eth", "db1.prod.acme.eth": "acme.eth", "acme.eth": "acme.eth",
	} {
		if got := orgFromName(in); got != want {
			t.Errorf("%s: %s", in, got)
		}
	}
}
