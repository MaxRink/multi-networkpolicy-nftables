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

func TestRuleEqual_EqualRules(t *testing.T) {
	a := makeRule("forward", "filter", []expr.Any{
		&expr.Meta{Key: expr.MetaKeyL4PROTO},
		&expr.Cmp{Op: expr.CmpOpEq, Data: []byte{0x06}},
		&expr.Verdict{Kind: expr.VerdictAccept},
	})
	b := makeRule("forward", "filter", []expr.Any{
		&expr.Meta{Key: expr.MetaKeyL4PROTO},
		&expr.Cmp{Op: expr.CmpOpEq, Data: []byte{0x06}},
		&expr.Verdict{Kind: expr.VerdictAccept},
	})

	if !ruleEqual(a, b) {
		t.Error("ruleEqual() returned false for identical rules; expected true")
	}
}

func TestRuleEqual_DifferentChain(t *testing.T) {
	exprs := []expr.Any{&expr.Verdict{Kind: expr.VerdictAccept}}
	a := makeRule("input", "filter", exprs)
	b := makeRule("output", "filter", exprs)

	if ruleEqual(a, b) {
		t.Error("ruleEqual() returned true for rules with different chain names; expected false")
	}
}

func TestRuleEqual_DifferentTable(t *testing.T) {
	exprs := []expr.Any{&expr.Verdict{Kind: expr.VerdictAccept}}
	a := makeRule("input", "filter", exprs)
	b := makeRule("input", "nat", exprs)

	if ruleEqual(a, b) {
		t.Error("ruleEqual() returned true for rules with different table names; expected false")
	}
}

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

func TestRuleEqual_EmptyExprs(t *testing.T) {
	a := makeRule("input", "filter", []expr.Any{})
	b := makeRule("input", "filter", []expr.Any{})

	if !ruleEqual(a, b) {
		t.Error("ruleEqual() returned false for rules with empty Exprs; expected true")
	}
}

func TestRuleEqual_CounterVsCounter(t *testing.T) {
	a := makeRule("input", "filter", []expr.Any{
		&expr.Counter{Bytes: 100, Packets: 10},
	})
	b := makeRule("input", "filter", []expr.Any{
		&expr.Counter{Bytes: 999, Packets: 99},
	})

	if !ruleEqual(a, b) {
		t.Error("ruleEqual() returned false for Counter vs Counter (different values); expected true (values are runtime-only)")
	}
}

func TestRuleEqual_CounterVsNonCounter(t *testing.T) {
	a := makeRule("input", "filter", []expr.Any{
		&expr.Counter{},
	})
	b := makeRule("input", "filter", []expr.Any{
		&expr.Verdict{Kind: expr.VerdictAccept},
	})

	if ruleEqual(a, b) {
		t.Error("ruleEqual() returned true for Counter vs non-Counter; expected false")
	}
}

func TestRuleEqual_UnknownExprType(t *testing.T) {
	a := makeRule("input", "filter", []expr.Any{
		&expr.Masq{},
	})
	b := makeRule("input", "filter", []expr.Any{
		&expr.Masq{},
	})

	if ruleEqual(a, b) {
		t.Error("ruleEqual() returned true for unhandled expression type; expected false (fail-safe default)")
	}
}
