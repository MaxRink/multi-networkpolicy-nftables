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
	"strings"
	"testing"
)

func TestTruncateNftName(t *testing.T) {
	tests := []struct {
		name    string
		input   string
		maxLen  int
		wantLen int // expected len of result, -1 = no constraint checked individually
		// wantExact is set when we know the exact expected output
		wantExact string
	}{
		{
			name:      "short name unchanged",
			input:     "simple",
			maxLen:    255,
			wantLen:   6,
			wantExact: "simple",
		},
		{
			name:    "name exactly at limit unchanged",
			input:   strings.Repeat("a", 255),
			maxLen:  255,
			wantLen: 255,
		},
		{
			name:    "overlong name truncated with hash suffix",
			input:   strings.Repeat("a", 300),
			maxLen:  255,
			wantLen: 255,
		},
		{
			name:    "overlong name with invalid chars - sanitized and truncated",
			input:   "very-long-namespace/very-long-name-with$invalid@chars!" + strings.Repeat("x", 300),
			maxLen:  255,
			wantLen: 255,
		},
		{
			name:      "invalid chars replaced with underscore in short name",
			input:     "hello/world",
			maxLen:    255,
			wantLen:   11,
			wantExact: "hello_world",
		},
		{
			name:      "maxLen zero returns empty string",
			input:     "anyname",
			maxLen:    0,
			wantLen:   0,
			wantExact: "",
		},
		{
			name:      "maxLen negative returns empty string",
			input:     "anyname",
			maxLen:    -1,
			wantLen:   0,
			wantExact: "",
		},
		{
			name:    "maxLen smaller than hash suffix returns hash truncated",
			input:   strings.Repeat("z", 300),
			maxLen:  5,
			wantLen: 5,
		},
		{
			name:    "maxLen equal to suffix length returns hash without dash (8 chars)",
			input:   strings.Repeat("z", 300),
			maxLen:  9, // suffix is "-XXXXXXXX" = 9 chars; maxLen<=len(suffix) so returns hash (8 chars)
			wantLen: 8,
		},
		{
			name:    "suffix is exactly 9 chars with zero-padded %08x",
			input:   strings.Repeat("b", 300),
			maxLen:  255,
			wantLen: 255,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := truncateNftName(tt.input, tt.maxLen)

			// Length check
			if len(result) != tt.wantLen {
				t.Errorf("truncateNftName(%q, %d) len = %d, want %d (result: %q)",
					tt.input[:min(len(tt.input), 30)], tt.maxLen, len(result), tt.wantLen, result)
			}

			// Exact value check
			if tt.wantExact != "" && result != tt.wantExact {
				t.Errorf("truncateNftName(%q, %d) = %q, want %q",
					tt.input, tt.maxLen, result, tt.wantExact)
			}

			result2 := truncateNftName(tt.input, tt.maxLen)
			if result != result2 {
				t.Errorf("truncateNftName is not deterministic: got %q then %q", result, result2)
			}

			// Result must not exceed maxLen
			if tt.maxLen > 0 && len(result) > tt.maxLen {
				t.Errorf("truncateNftName(%q, %d) result length %d exceeds maxLen",
					tt.input[:min(len(tt.input), 30)], tt.maxLen, len(result))
			}

			// Result must only contain valid nft chars (when non-empty)
			if len(result) > 0 {
				for i, r := range result {
					if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') ||
						(r >= '0' && r <= '9') || r == '_' || r == '-' || r == '.' {
						continue
					}
					t.Errorf("truncateNftName result contains invalid char %q at pos %d: %q", r, i, result)
					break
				}
			}
		})
	}
}

func TestTruncateNftNameDeterministicSuffix(t *testing.T) {
	// Verify that overlong names with the same content produce the same suffix (deterministic hash).
	name := "very-long-namespace/overlong-name-that-exceeds-limit" + strings.Repeat("x", 250)
	result1 := truncateNftName(name, 255)
	result2 := truncateNftName(name, 255)

	if result1 != result2 {
		t.Errorf("truncateNftName is not deterministic: %q vs %q", result1, result2)
	}
	if len(result1) != 255 {
		t.Errorf("expected result length 255, got %d", len(result1))
	}

	// Suffix should be exactly 9 chars: dash + 8 hex digits
	suffix := result1[len(result1)-9:]
	if suffix[0] != '-' {
		t.Errorf("expected suffix to start with '-', got %q", suffix)
	}
	for _, c := range suffix[1:] {
		if (c < '0' || c > '9') && (c < 'a' || c > 'f') {
			t.Errorf("suffix contains non-hex char %q in %q", c, suffix)
		}
	}
}

func TestNftNameWithSuffixReservesSuffixBudget(t *testing.T) {
	longBase := strings.Repeat("policy-", 80)
	tests := []struct {
		name      string
		separator string
		suffix    string
	}{
		{
			name:      "ports chain",
			separator: "-",
			suffix:    portsChainSuffix + "-12",
		},
		{
			name:      "peers chain",
			separator: "-",
			suffix:    peersChainSuffix + "-34",
		},
		{
			name:      "tcp set",
			separator: "_",
			suffix:    "tcp",
		},
		{
			name:      "udp set",
			separator: "_",
			suffix:    "udp",
		},
		{
			name:      "sctp set",
			separator: "_",
			suffix:    "sctp",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := nftNameWithSuffix(longBase, tt.separator, tt.suffix)
			if len(result) > nftNameMaxLen {
				t.Fatalf("nftNameWithSuffix result length %d exceeds %d: %q", len(result), nftNameMaxLen, result)
			}
			wantSuffix := tt.separator + tt.suffix
			if !strings.HasSuffix(result, wantSuffix) {
				t.Fatalf("nftNameWithSuffix result %q does not preserve suffix %q", result, wantSuffix)
			}
		})
	}
}

func TestPolicyChildNftNamesStayWithinLimit(t *testing.T) {
	policyChainName := truncateNftName(strings.Repeat("very-long-policy-", 40), nftNameMaxLen)
	portsName := nftNameWithSuffix(policyChainName, "-", portsChainSuffix+"-123")
	peersName := nftNameWithSuffix(policyChainName, "-", peersChainSuffix+"-456")

	for name, value := range map[string]string{
		"ports chain": portsName,
		"peers chain": peersName,
		"tcp set":     nftNameWithSuffix(getSetName(portsName), "_", "tcp"),
		"udp set":     nftNameWithSuffix(getSetName(portsName), "_", "udp"),
		"sctp set":    nftNameWithSuffix(getSetName(portsName), "_", "sctp"),
	} {
		if len(value) > nftNameMaxLen {
			t.Fatalf("%s length %d exceeds %d: %q", name, len(value), nftNameMaxLen, value)
		}
	}
}

func TestUserDataCommentMaxLen(t *testing.T) {
	// A comment within limit passes through unchanged.
	short := strings.Repeat("a", 100)
	ud := userDataComment(short)
	// userdata TLV: [type][len][data...][null]
	// type=0x00, len=101 (100+null), then 100 'a' bytes + null
	if len(ud) != 2+len(short)+1 {
		t.Errorf("unexpected userdata length for short comment: %d", len(ud))
	}

	// A comment exactly at limit (254 bytes) is allowed.
	atLimit := strings.Repeat("b", userDataCommentMaxLen)
	ud2 := userDataComment(atLimit)
	// length field should be 255 (254 + null)
	if ud2[1] != 255 {
		t.Errorf("expected length byte 255, got %d", ud2[1])
	}

	// A comment exceeding the limit is truncated to 254 bytes.
	overlong := strings.Repeat("c", 300)
	ud3 := userDataComment(overlong)
	// length field should be 255 (254 + null), not overflow
	if ud3[1] != 255 {
		t.Errorf("expected length byte 255 for overlong comment, got %d", ud3[1])
	}
	if len(ud3) != 2+userDataCommentMaxLen+1 {
		t.Errorf("expected userdata length %d, got %d", 2+userDataCommentMaxLen+1, len(ud3))
	}
}
