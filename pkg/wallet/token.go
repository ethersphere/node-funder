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
	// Goerli (testnet)
	5: {
		Contract: common.HexToAddress("0x2aC3c1d3e24b45c6C310534Bc2Dd84B5ed576335"),
		Symbol:   "gBZZ",
		Decimals: 16,
	},

	// Gnosis Chain
	100: {
		Contract: common.HexToAddress("0xdBF3Ea6F5beE45c02255B2c26a16F300502F68da"),
		Symbol:   "xBZZ",
		Decimals: 16,
	},
}

var chainToNativeCoinMap = map[int64]Token{
	// Goerli (testnet)
	5: {
		Decimals: 16,
	},

	// Gnosis Chain
	100: {
		Decimals: 16,
	},
}

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
