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
		switch rule.Exprs[i].(type) {
		case *expr.Meta, *expr.Lookup, *expr.Verdict, *expr.Cmp,
			*expr.Payload, *expr.Ct, *expr.Bitwise:
			v, err := hash(rule.Exprs[i])
			if err != nil {
				return nil, fmt.Errorf("failed to hash expression: %w", err)
			}
			newExprs = append(newExprs, v)
		}
	}

	data.Exprs = newExprs
	return data, nil
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
