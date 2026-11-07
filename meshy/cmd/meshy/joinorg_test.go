package main

import "testing"

func TestParseInviteV2(t *testing.T) {
	raw := make([]byte, 91)
	raw[0], raw[1] = 2, 0
	raw = append(raw, []byte("night-test.meshy.eth\x00alice")...)
	b, err := parseInvite(raw)
	if err != nil || b.Org != "night-test.meshy.eth" || b.Inv.Label != "alice" || b.Role != "member" {
		t.Fatalf("%+v %v", b, err)
	}
	raw[1] = 3 // agent, with invitee
	raw = append(raw[:91], append(make([]byte, 20), []byte("acme.meshy.eth\x00")...)...)
	b, err = parseInvite(raw)
	if err != nil || b.Org != "acme.meshy.eth" || b.Role != "agent" || b.Inv.Label != "" {
		t.Fatalf("%+v %v", b, err)
	}
	if _, err := parseInvite([]byte(`{"org":"x.eth","role":"member","inv":{"label":"bob"}}`)); err != nil {
		t.Fatal("v1 links stopped working")
	}
}
