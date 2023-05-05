// Copyright 2022 The Swarm Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package funder_test

import (
	"context"
	"math/big"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"

	. "github.com/ethersphere/node-funder/pkg/funder"
	fundermock "github.com/ethersphere/node-funder/pkg/funder/mock"
	"github.com/ethersphere/node-funder/pkg/wallet"
	walletmock "github.com/ethersphere/node-funder/pkg/wallet/mock"
)

func Test_Fund(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	bc := walletmock.NewBackendClient()
	key := generateKey(t)
	w := wallet.New(bc, key)

	t.Run("fund addresses - empty", func(t *testing.T) {
		t.Parallel()

		cfg := Config{}
		err := Fund(ctx, cfg, nil, w)
		assert.NoError(t, err)
	})

	t.Run("fund addresses - set", func(t *testing.T) {
		t.Parallel()

		t.Run("already funded (0,0)", func(t *testing.T) {
			t.Parallel()

			cfg := Config{Addresses: []string{"0x95f8916183f7C7154e49396507F5b0FafA4d8077"}}
			err := Fund(ctx, cfg, nil, w)
			assert.NoError(t, err)
		})

		t.Run("not funded (3,3)", func(t *testing.T) {
			cfg := Config{
				Addresses: []string{
					"0x95f8916183f7C7154e49396507F5b0FafA4d8077",
					"0x95f8916183f7C7154e49396507F5b0FafA4d8071",
					"0x95f8916183f7C7154e49396507F5b0FafA4d8072",
					"0x95f8916183f7C7154e49396507F5b0FafA4d8073",
				},
				MinAmounts: MinAmounts{NativeCoin: 3, SwarmToken: 3},
			}
			err := Fund(ctx, cfg, nil, w)
			assert.NoError(t, err)
		})
	})

	t.Run("fund namespace - empty", func(t *testing.T) {
		t.Parallel()

		nl := fundermock.NewNodeLister(nil)
		cfg := Config{Namespace: "swarm"}
		err := Fund(ctx, cfg, nl, w)
		assert.NoError(t, err)
	})

	t.Run("fund namespace - not a bee node", func(t *testing.T) {
		t.Parallel()

		nl := fundermock.NewNodeLister([]NodeInfo{{Address: "addr"}})
		cfg := Config{Namespace: "swarm"}
		err := Fund(ctx, cfg, nl, w)
		assert.NoError(t, err)
	})

	t.Run("fund namespace - valid bee node", func(t *testing.T) {
		t.Parallel()

		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
			_, err := w.Write([]byte(`
			{
				"walletAddress": "0x95f8916183f7C7154e49396507F5b0FafA4d8077",
				"chainID": 100
			}
			`))
			assert.NoError(t, err)
		}))
		t.Cleanup(server.Close)

		t.Run("already funded (0,0)", func(t *testing.T) {
			t.Parallel()

			nl := fundermock.NewNodeLister([]NodeInfo{{Address: server.URL}})
			cfg := Config{Namespace: "swarm"}
			err := Fund(ctx, cfg, nl, w)
			assert.NoError(t, err)
		})

		t.Run("already funded (1,1)", func(t *testing.T) {
			t.Parallel()

			nl := fundermock.NewNodeLister([]NodeInfo{{Address: server.URL}})
			cfg := Config{Namespace: "swarm", MinAmounts: MinAmounts{NativeCoin: 1, SwarmToken: 1}}
			err := Fund(ctx, cfg, nl, w)
			assert.NoError(t, err)
		})

		t.Run("not funded (3,3)", func(t *testing.T) {
			t.Parallel()

			nl := fundermock.NewNodeLister([]NodeInfo{{Address: server.URL}})
			cfg := Config{Namespace: "swarm", MinAmounts: MinAmounts{NativeCoin: 3, SwarmToken: 3}}
			err := Fund(ctx, cfg, nl, w)
			assert.NoError(t, err)
		})
	})
}

func Test_CalcTopUpAmount(t *testing.T) {
	t.Parallel()

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
		got := CalcTopUpAmount(tc.min, toBigInt(tc.currAmount), tc.tokenDecimals)
		assert.Equal(t, tc.expected, got.String())
	}
}

func Test_FormatAmount(t *testing.T) {
	t.Parallel()

	assert.Equal(t, "0", FormatAmount(nil, 0))
	assert.Equal(t, "0", FormatAmount(nil, 10))
	assert.Equal(t, "1000", FormatAmount(big.NewInt(1000), 0))
	assert.Equal(t, "10", FormatAmount(big.NewInt(1000), 2))
	assert.Equal(t, "10.1", FormatAmount(big.NewInt(1010), 2))
	assert.Equal(t, "10.01", FormatAmount(big.NewInt(1001), 2))
}

func toBigInt(val string) *big.Int {
	bi := new(big.Int)
	bi.SetString(val, 10)

	return bi
}

func generateKey(t *testing.T) wallet.Key {
	t.Helper()

	key, err := wallet.GenerateKey()
	assert.NoError(t, err)

	return key
}
