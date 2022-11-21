// Copyright 2022 The Swarm Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package funder

import (
	"flag"
	"fmt"
)

type Config struct {
	ChainNodeEndpoint string
	Namespace         string
	MinAmounts        MinAmounts
}

type MinAmounts struct {
	NativeCoin float64 // on mainnet this is ETH
	SwarmToken float64 // on mainnet this is BZZ
}

func ParseConfig() (Config, error) {
	cfg := Config{}

	flag.StringVar(&cfg.Namespace, "namespace", "", "kuberneties namespace")
	flag.StringVar(&cfg.ChainNodeEndpoint, "chainNodeEndpoint", "", "endpoint to chain node")
	flag.Float64Var(&cfg.MinAmounts.NativeCoin, "minNative", 0, "specifies min amout of chain native coins (ETH) nodes should have")
	flag.Float64Var(&cfg.MinAmounts.SwarmToken, "minSwarm", 0, "specifies min amout of swarm tokens (BZZ) nodes should have")
	flag.Parse()

	if cfg.Namespace == "" {
		return cfg, fmt.Errorf("namespace must be set")
	}

	if cfg.ChainNodeEndpoint == "" {
		return cfg, fmt.Errorf("url to chain node must be set")
	}

	return cfg, nil
}
