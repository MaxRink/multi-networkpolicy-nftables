/*
Copyright 2025 Deutsche Telekom AG.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package server

import (
	"bytes"
	"encoding/gob"
	"fmt"
	"hash/fnv"

	"github.com/google/nftables"
	"github.com/google/nftables/expr"
)

type ruleData struct {
	Table    string
	Chain    string
	Family   nftables.TableFamily
	UserData []byte
	Exprs    []string
}

// func hashRule(rule *nftables.Rule) (string, error) {
// 	data, err := newRuleData(rule)
// 	if err != nil {
// 		return "", fmt.Errorf("failed to hash rule: %w", err)
// 	}
// 	return hash(data)
// }

func newRuleData(rule *nftables.Rule) (*ruleData, error) {
	data := &ruleData{
		Table:    rule.Table.Name,
		Chain:    rule.Chain.Name,
		Family:   rule.Table.Family,
		UserData: bytes.Clone(rule.UserData),
	}

	newExprs := []string{}
	for i := range rule.Exprs {
		e := canonicalExpr(rule.Exprs[i])
		switch e.(type) {
		case *expr.Meta, *expr.Lookup, *expr.Verdict, *expr.Cmp,
			*expr.Payload, *expr.Ct, *expr.Bitwise:
			v, err := hash(e)
			if err != nil {
				return nil, fmt.Errorf("failed to hash expression: %w", err)
			}
			newExprs = append(newExprs, v)
		}
	}

	data.Exprs = newExprs
	return data, nil
}

// canonicalExpr returns e as the kernel dumps it, so that a rule hashes and
// compares equal to its kernel copy. Otherwise the cleanup after an apply
// deletes the rule as stale, and the next apply adds it again.
func canonicalExpr(e expr.Any) expr.Any {
	switch v := e.(type) {
	case *expr.Lookup:
		c := *v
		// SetID only identifies a set created in the same batch; the
		// kernel does not return it.
		c.SetID = 0
		c.SourceRegister, c.DestRegister = canonicalRegister(c.SourceRegister), canonicalRegister(c.DestRegister)
		return &c
	case *expr.Payload:
		c := *v
		c.SourceRegister, c.DestRegister = canonicalRegister(c.SourceRegister), canonicalRegister(c.DestRegister)
		return &c
	case *expr.Meta:
		c := *v
		c.Register = canonicalRegister(c.Register)
		return &c
	case *expr.Cmp:
		c := *v
		c.Register = canonicalRegister(c.Register)
		return &c
	case *expr.Bitwise:
		c := *v
		c.SourceRegister, c.DestRegister = canonicalRegister(c.SourceRegister), canonicalRegister(c.DestRegister)
		return &c
	case *expr.Ct:
		c := *v
		c.Register = canonicalRegister(c.Register)
		return &c
	}
	return e
}

// canonicalRegister returns the register number the kernel dumps for reg. A
// 32-bit register that starts a 128-bit one, NFT_REG32_00 (8), _04, _08 or
// _12, is dumped as that 128-bit register, NFT_REG_1 (1) to NFT_REG_4; see
// nft_dump_register in net/netfilter/nf_tables_api.c.
func canonicalRegister(reg uint32) uint32 {
	const reg32First, reg32Last = 8, 23 // NFT_REG32_00, NFT_REG32_15
	if reg >= reg32First && reg <= reg32Last && (reg-reg32First)%4 == 0 {
		return (reg-reg32First)/4 + 1
	}
	return reg
}

func hash(data any) (string, error) {
	var buf bytes.Buffer // Stand-in for a network connection
	enc := gob.NewEncoder(&buf)

	d, ok := data.(*nftables.Rule)
	if ok {
		tmp, err := newRuleData(d)
		if err != nil {
			return "", fmt.Errorf("failed to prepare rule data: %w", err)
		}
		data = tmp
	}

	if err := enc.Encode(data); err != nil {
		return "", fmt.Errorf("failed to encode struct: %w", err)
	}

	h := fnv.New32a()
	if _, err := h.Write(buf.Bytes()); err != nil {
		return "", fmt.Errorf("failed to generate hash: %w", err)
	}

	return fmt.Sprintf("%x", h.Sum32()), nil
}
