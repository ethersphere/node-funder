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
	Name     string
	Decimals int
}

var chainToSwarmTokenMap = map[int64]Token{
	// Goerli
	5: {
		Contract: common.HexToAddress(""), // TODO
		Decimals: 18,                      // TODO
	},

	// Xdai
	100: {
		Contract: common.HexToAddress(""), // TODO
		Decimals: 18,                      // TODO
	},
}

var chainToNativeCoinMap = map[int64]Token{
	// Goerli
	5: {
		Decimals: 18,
	},

	// Xdai
	100: {
		Decimals: 18,
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
