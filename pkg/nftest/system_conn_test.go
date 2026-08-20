package nftest

import (
	"errors"
	"testing"

	"github.com/google/nftables"
	"github.com/mdlayher/netlink"
)

func TestCheckNftablesLivenessAllowsEmptyTableFamily(t *testing.T) {
	var requests int
	conn, err := nftables.New(nftables.WithTestDial(func(req []netlink.Message) ([]netlink.Message, error) {
		requests += len(req)
		return nil, nil
	}))
	if err != nil {
		t.Fatalf("nftables.New() failed: %v", err)
	}

	if err := checkNftablesLiveness(conn); err != nil {
		t.Fatalf("checkNftablesLiveness() failed for an empty table family: %v", err)
	}
	if requests != 1 {
		t.Fatalf("nftables liveness probe sent %d requests, want 1", requests)
	}
}

func TestCheckNftablesLivenessReturnsNetlinkError(t *testing.T) {
	wantErr := errors.New("nftables unavailable")
	conn, err := nftables.New(nftables.WithTestDial(func([]netlink.Message) ([]netlink.Message, error) {
		return nil, wantErr
	}))
	if err != nil {
		t.Fatalf("nftables.New() failed: %v", err)
	}

	if err := checkNftablesLiveness(conn); !errors.Is(err, wantErr) {
		t.Fatalf("checkNftablesLiveness() error = %v, want %v", err, wantErr)
	}
}

func TestShouldSkipUnavailable(t *testing.T) {
	tests := []struct {
		name  string
		value string
		want  bool
	}{
		{name: "empty", value: "", want: false},
		{name: "one", value: "1", want: true},
		{name: "true", value: "true", want: true},
		{name: "upper true", value: "TRUE", want: true},
		{name: "zero", value: "0", want: false},
		{name: "false", value: "false", want: false},
		{name: "invalid", value: "not-bool", want: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Setenv(skipUnavailableEnvVar, tt.value)
			if got := shouldSkipUnavailable(); got != tt.want {
				t.Fatalf("shouldSkipUnavailable() = %v, want %v (value %q)", got, tt.want, tt.value)
			}
		})
	}
}
