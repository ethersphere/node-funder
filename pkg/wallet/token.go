// Copyright 2022 The Swarm Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package wallet

import (
	"fmt"

	"github.com/ethereum/go-ethereum/common"
)

type Token struct {
	Contract common.Address
	Symbol   string
	Decimals int
}

var chainToSwarmTokenMap = map[int64]Token{
	// Testnet
	5: {
		Contract: common.HexToAddress("0x2aC3c1d3e24b45c6C310534Bc2Dd84B5ed576335"),
		Symbol:   "gBZZ",
		Decimals: 16,
	},

	// Mainnet
	100: {
		Contract: common.HexToAddress("0x19062190b1925b5b6689d7073fdfc8c2976ef8cb"),
		Symbol:   "xBZZ",
		Decimals: 16,
	},
}

var chainToNativeCoinMap = map[int64]Token{
	// Testnet
	5: {
		Symbol:   "ETH",
		Decimals: 18,
	},

	// Mainnet
	100: {
		Symbol:   "xDAI",
		Decimals: 18,
	},
}

type TokenInfoGetterFn = func(cid int64) (Token, error)

func SwarmTokenForChain(cid int64) (Token, error) {
	if t, ok := chainToSwarmTokenMap[cid]; ok {
		return t, nil
	}

	return Token{}, fmt.Errorf("swarm token not specified for chain (id %d)", cid)
}

func NativeCoinForChain(cid int64) (Token, error) {
	if t, ok := chainToNativeCoinMap[cid]; ok {
		return t, nil
	}

	return Token{}, fmt.Errorf("native coin not specified for chain (id %d)", cid)
}
