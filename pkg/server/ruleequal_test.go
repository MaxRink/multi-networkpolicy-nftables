package server

import (
	"testing"

	nftables "github.com/google/nftables"
	"github.com/google/nftables/expr"
)

func makeRule(chainName, tableName string, exprs []expr.Any) *nftables.Rule {
	return &nftables.Rule{
		Chain: &nftables.Chain{Name: chainName},
		Table: &nftables.Table{Name: tableName},
		Exprs: exprs,
	}
}

// TestRuleEqual_DifferentExprLen verifies that ruleEqual returns false (without
// panicking) when a.Exprs and b.Exprs have different lengths.
func TestRuleEqual_DifferentExprLen(t *testing.T) {
	a := makeRule("input", "filter", []expr.Any{
		&expr.Meta{Key: expr.MetaKeyL4PROTO},
		&expr.Cmp{Op: expr.CmpOpEq},
	})
	b := makeRule("input", "filter", []expr.Any{
		&expr.Meta{Key: expr.MetaKeyL4PROTO},
	})

	if ruleEqual(a, b) {
		t.Error("ruleEqual() returned true for rules with different Exprs lengths; expected false")
	}
}

// TestRuleEqual_EqualRules verifies that ruleEqual returns true for two
// identical rules.
func TestRuleEqual_EqualRules(t *testing.T) {
	exprs := []expr.Any{
		&expr.Meta{Key: expr.MetaKeyL4PROTO},
		&expr.Cmp{Op: expr.CmpOpEq, Data: []byte{0x06}},
		&expr.Verdict{Kind: expr.VerdictAccept},
	}
	a := makeRule("forward", "filter", exprs)
	b := makeRule("forward", "filter", exprs)
	b.Exprs = []expr.Any{
		&expr.Meta{Key: expr.MetaKeyL4PROTO},
		&expr.Cmp{Op: expr.CmpOpEq, Data: []byte{0x06}},
		&expr.Verdict{Kind: expr.VerdictAccept},
	}

	if !ruleEqual(a, b) {
		t.Error("ruleEqual() returned false for identical rules; expected true")
	}
}

// TestRuleEqual_DifferentChain verifies that ruleEqual returns false when
// chain names differ.
func TestRuleEqual_DifferentChain(t *testing.T) {
	exprs := []expr.Any{&expr.Verdict{Kind: expr.VerdictAccept}}
	a := makeRule("input", "filter", exprs)
	b := makeRule("output", "filter", exprs)

	if ruleEqual(a, b) {
		t.Error("ruleEqual() returned true for rules with different chain names; expected false")
	}
}

// TestRuleEqual_DifferentTable verifies that ruleEqual returns false when
// table names differ.
func TestRuleEqual_DifferentTable(t *testing.T) {
	exprs := []expr.Any{&expr.Verdict{Kind: expr.VerdictAccept}}
	a := makeRule("input", "filter", exprs)
	b := makeRule("input", "nat", exprs)

	if ruleEqual(a, b) {
		t.Error("ruleEqual() returned true for rules with different table names; expected false")
	}
}

// TestRuleEqual_DifferentUserData verifies that ruleEqual returns false when
// UserData differs.
func TestRuleEqual_DifferentUserData(t *testing.T) {
	exprs := []expr.Any{&expr.Verdict{Kind: expr.VerdictAccept}}
	a := makeRule("input", "filter", exprs)
	a.UserData = []byte("comment:a")
	b := makeRule("input", "filter", exprs)
	b.UserData = []byte("comment:b")

	if ruleEqual(a, b) {
		t.Error("ruleEqual() returned true for rules with different UserData; expected false")
	}
}

// TestRuleEqual_EmptyExprs verifies that ruleEqual handles empty Exprs slices
// correctly.
func TestRuleEqual_EmptyExprs(t *testing.T) {
	a := makeRule("input", "filter", []expr.Any{})
	b := makeRule("input", "filter", []expr.Any{})

	if !ruleEqual(a, b) {
		t.Error("ruleEqual() returned false for rules with empty Exprs; expected true")
	}
}
