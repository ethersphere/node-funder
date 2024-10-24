// Copyright 2022 The Swarm Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package wallet

import (
	"fmt"

	"github.com/ethereum/go-ethereum/common"
)

const (
	SwarmTokenDecimals = 16
	LocalnetChainID    = 12345
)

type Token struct {
	Contract common.Address
	Symbol   string
	Decimals int
}

var chainToSwarmTokenMap = map[int64]Token{
	// Sepolia Testnet
	11155111: {
		Contract: common.HexToAddress("0x543dDb01Ba47acB11de34891cD86B675F04840db"),
		Symbol:   "sBZZ",
		Decimals: SwarmTokenDecimals,
	},

	// Gnosis Mainnet
	100: {
		Contract: common.HexToAddress("0xdBF3Ea6F5beE45c02255B2c26a16F300502F68da"),
		Symbol:   "xBZZ",
		Decimals: SwarmTokenDecimals,
	},

	// Localnet
	LocalnetChainID: {
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
	LocalnetChainID: {
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
