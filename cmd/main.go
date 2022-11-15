// Copyright 2022 The Swarm Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package main

import (
	"fmt"

	"github.com/ethersphere/node-funder/pkg/funder"
)

func main() {
	cfg, err := funder.ParseConfig()
	if err != nil {
		panic(fmt.Errorf("failed parsing config: %w", err))
	}

	if err = funder.FundAllNodes(cfg); err != nil {
		panic(fmt.Errorf("error while funding nodes: %w", err))
	}
}
