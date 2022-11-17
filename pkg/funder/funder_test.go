package funder_test

import (
	"math/big"
	"testing"

	"github.com/ethersphere/node-funder/pkg/funder"
)

func Test_CalcTopUpAmount(t *testing.T) {
	tests := []struct {
		min           float64
		currAmount    string
		tokenDecimals int
		expected      string
	}{
		{
			min:           2.4,
			currAmount:    "1000000000000000000",
			tokenDecimals: 18,
			expected:      "1400000000000000000",
		},
		{
			min:           2.4,
			currAmount:    "3000000000000000000",
			tokenDecimals: 18,
			expected:      "-600000000000000000",
		},
	}

	for _, tc := range tests {
		got := funder.CalcTopUpAmount(tc.min, toBigInt(tc.currAmount), tc.tokenDecimals)
		if got.String() != tc.expected {
			t.Fatalf("got %s, want %s", got, tc.expected)
		}
	}
}

func toBigInt(val string) *big.Int {
	bi := new(big.Int)
	bi.SetString(val, 10)
	return bi
}
