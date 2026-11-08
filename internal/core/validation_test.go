package core

import (
	"errors"
	"strings"
	"testing"
)

func TestRuleErrorsIdentifyVisibleRuleAndField(t *testing.T) {
	cases := []struct{ name, second, field string }{
		{"empty", `{"domains":[],"action":"direct"}`, "matchers"},
		{"server", `{"domains":["two.test"],"action":"proxy"}`, "action"},
		{"ip", `{"ips":["999.1.1.1"],"action":"block"}`, "ips"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := ReadConfig(strings.NewReader(`{"rules":[{"domains":["one.test"],"action":"direct"},` + tc.second + `]}`))
			var issue *RuleValidationError
			if !errors.As(err, &issue) || issue.RuleIndex != 1 || issue.Field != tc.field || !strings.HasPrefix(err.Error(), "Rule 2:") {
				t.Fatalf("wrong editor location: %#v %v", issue, err)
			}
		})
	}
}

func TestSetOnlyRuleAndZapretCompatibility(t *testing.T) {
	cfg, err := ReadConfig(strings.NewReader(`{"wanInterface":"eth0","zapretArgs":["--filter-tcp=80,443","--dpi-desync=fake"],"rules":[{"sets":["mixed:service"],"domains":[],"action":"zapret"}]}`))
	if err != nil {
		t.Fatal("set-only rule was reported empty:", err)
	}
	path := geoFixture(t)
	if err = ValidateRuleSets(cfg, path); err != nil {
		t.Fatal(err)
	}
	cfg.Rules[0].Sets = []string{"geosite:regex"}
	err = ValidateRuleSets(cfg, path)
	var issue *RuleValidationError
	if !errors.As(err, &issue) || issue.Field != "sets" || !strings.Contains(issue.Message, "geosite:regex") {
		t.Fatal("incompatible set lacks field and name:", err)
	}
	cfg.Rules[0].Action = "direct"
	if err = ValidateRuleSets(cfg, path); err != nil {
		t.Fatal("Xray set rejected:", err)
	}
	cfg.Rules[0].Sets = []string{"mixed:missing"}
	if err = ValidateRuleSets(cfg, path); !errors.As(err, &issue) {
		t.Fatal("missing set lacks structured error:", err)
	}
}
