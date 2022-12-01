// Copyright 2022 The Swarm Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package funder

import (
	"flag"
	"fmt"
	"strings"
)

type Config struct {
	Namespace         string
	Addresses         []string
	ChainNodeEndpoint string
	WalletKey         string // Hex encoded key
	MinAmounts        MinAmounts
}

type MinAmounts struct {
	NativeCoin float64 // on mainnet this is ETH
	SwarmToken float64 // on mainnet this is BZZ
}

func ParseConfig() (Config, error) {
	cfg := Config{}

	var addressesStr string

	flag.StringVar(&cfg.Namespace, "namespace", "", "kuberneties namespace")
	flag.StringVar(&addressesStr, "addresses", "", "wallet addresses")
	flag.StringVar(&cfg.ChainNodeEndpoint, "chainNodeEndpoint", "", "endpoint to chain node")
	flag.StringVar(&cfg.WalletKey, "walletKey", "", "wallet key")
	flag.Float64Var(&cfg.MinAmounts.NativeCoin, "minNative", 0, "specifies min amout of chain native coins (ETH) nodes should have")
	flag.Float64Var(&cfg.MinAmounts.SwarmToken, "minSwarm", 0, "specifies min amout of swarm tokens (BZZ) nodes should have")
	flag.Parse()

	cfg.Addresses = parseAddresses(addressesStr)

	if cfg.Namespace == "" && len(cfg.Addresses) == 0 {
		return cfg, fmt.Errorf("-namespace or -addresses must be set")
	}

	if cfg.ChainNodeEndpoint == "" {
		return cfg, fmt.Errorf("-chainNodeEndpoint must be set")
	}

	if cfg.WalletKey == "" {
		return cfg, fmt.Errorf("-walletKey must be set")
	}

	return cfg, nil
}

func parseAddresses(s string) []string {
	if s == "" {
		return nil
	}

	return strings.Split(s, ",")
}
