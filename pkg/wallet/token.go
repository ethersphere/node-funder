// Copyright 2022 The Swarm Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package wallet

import (
	"fmt"

	"github.com/ethereum/go-ethereum/common"
)

const SwarmTokenDecimals = 16

type Token struct {
	Contract common.Address
	Symbol   string
	Decimals int
}

var chainToSwarmTokenMap = map[int64]Token{
	// Sepolia Testnet
	11155111: {
		Contract: common.HexToAddress("0xa66be4A7De4DfA5478Cb2308469D90115C45aA23"),
		Symbol:   "sBZZ",
		Decimals: SwarmTokenDecimals,
	},

	// Mainnet
	100: {
		Contract: common.HexToAddress("0x19062190b1925b5b6689d7073fdfc8c2976ef8cb"),
		Symbol:   "xBZZ",
		Decimals: SwarmTokenDecimals,
	},

	// Localnet
	12345: {
		Contract: common.HexToAddress("0x6aab14fe9cccd64a502d23842d916eb5321c26e7"),
		Symbol:   "tBZZ",
		Decimals: SwarmTokenDecimals,
	},
}

var chainToNativeCoinMap = map[int64]Token{
	// Sepolia Testnet
	11155111: {
		Symbol:   "sETH",
		Decimals: 18,
	},

	// Mainnet
	100: {
		Symbol:   "xDAI",
		Decimals: 18,
	},

	// Localnet
	12345: {
		Symbol:   "tETH",
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
