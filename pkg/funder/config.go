// Copyright 2022 The Swarm Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package funder

import (
	"flag"
	"fmt"
)

type Config struct {
	Namespace     string
	MinNativeCoin float64 // on mainnet this is ETH
	MinBZZToken   float64 // on mainnet this is BZZ
}

func ParseConfig() (Config, error) {
	cfg := Config{}

	flag.StringVar(&cfg.Namespace, "namespace", "", "kuberneties namespace")
	flag.Float64Var(&cfg.MinNativeCoin, "minNativ", 0, "specifies min amout of ETH tokens nodes should have")
	flag.Float64Var(&cfg.MinBZZToken, "minBzz", 0, "specifies min amout of BZZ tokens nodes should have")
	flag.Parse()

	if cfg.Namespace == "" {
		return cfg, fmt.Errorf("namespace must be set")
	}

	return cfg, nil
}
