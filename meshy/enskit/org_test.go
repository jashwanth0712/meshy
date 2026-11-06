package enskit

import "testing"

func TestOrgUnderSharedParent(t *testing.T) {
	for in, want := range map[string]string{
		"db1.prod.acme.eth":              "acme.eth",
		"acme.eth":                       "acme.eth",
		"claude-1.agents.acme.meshy.eth": "acme.meshy.eth",
		"db1.prod.acme.meshy.eth":        "acme.meshy.eth",
		"g.db1.prod.acme.meshy.eth":      "acme.meshy.eth",
		"acme.meshy.eth":                 "acme.meshy.eth",
		"meshy.eth":                      "meshy.eth",
	} {
		if got := string(Name(in).Org()); got != want {
			t.Errorf("%s: Org()=%s want %s", in, got, want)
		}
	}
	if d := Name("db1.prod.acme.meshy.eth").Depth(); d != 2 {
		t.Errorf("depth %d", d)
	}
	if _, err := GrantName("claude-1", Name("db1.prod.acme.meshy.eth")); err != nil {
		t.Error(err)
	}
}
