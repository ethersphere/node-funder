// Copyright 2022 The Swarm Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package funder

type Config struct {
	Namespace         string
	Addresses         []string
	ChainNodeEndpoint string
	WalletKey         string // Hex encoded key
	MinAmounts        MinAmounts
}

type MinAmounts struct {
	NativeCoin float64 // on mainnet this is xDAI
	SwarmToken float64 // on mainnet this is xBZZ
}
