package core

// Nil Conditions preserves compatibility: all existing matcher fields are active.
type RuleConditions struct {
	Domains bool `json:"domains"`
	IPs     bool `json:"ips"`
	Sets    bool `json:"sets"`
}

func (r Rule) Active() Rule {
	if r.Conditions != nil {
		if !r.Conditions.Domains {
			r.Domains = nil
		}
		if !r.Conditions.IPs {
			r.IPs = nil
		}
		if !r.Conditions.Sets {
			r.Sets = nil
		}
	}
	r.Conditions = nil
	if r.Action != "proxy" {
		r.Node = ""
		r.NodeRef = ""
		r.NodeRefs = nil
		r.Selection = nil
	} else if len(r.NodeRefs) < 2 {
		r.Selection = nil
	} else if r.Selection != nil {
		selection := *r.Selection
		if selection.Mode != "url" {
			selection.URL = ""
		}
		r.Selection = &selection
	}
	return r
}

func (c Config) Active() Config {
	rules := make([]Rule, len(c.Rules))
	for i, r := range c.Rules {
		rules[i] = r.Active()
	}
	c.Rules = rules
	return c
}
