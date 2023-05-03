// Copyright 2022 The Swarm Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package main

import (
	"context"
	"log"

	"github.com/ethersphere/node-funder/pkg/funder"
)

func main() {
	cfg, err := funder.ParseConfig()
	if err != nil {
		log.Fatalf("failed parsing config: %v", err)
	}

	ctx := context.Background()
	nl, err := funder.NewNodeLister()
	if err != nil {
		log.Fatalf("could not create node lister: %v", err)
	}

	if err = funder.Fund(ctx, cfg, nl); err != nil {
		log.Fatalf("error while funding: %v", err)
	}
}
