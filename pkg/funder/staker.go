// Copyright 2023 The Swarm Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package funder

import (
	"context"
	"encoding/json"
	"fmt"
	"math/big"
	"net/http"
	"strings"
	"sync"
	"sync/atomic"

	"github.com/ethersphere/bee/v2/pkg/bigint"
	"github.com/ethersphere/beekeeper/pkg/logging"
	"github.com/ethersphere/node-funder/pkg/wallet"
	"k8s.io/utils/strings/slices"
)

func Stake(ctx context.Context, cfg Config, nl NodeLister, options ...FunderOptions) error {
	opts := DefaultOptions()
	for _, opt := range options {
		opt(opts)
	}

	opts.log.Infof("node staking started...")
	defer opts.log.Info("node staking finished")

	if nl == nil {
		var err error

		nl, err = newNodeLister()
		if err != nil {
			return fmt.Errorf("create node lister: %w", err)
		}
	}

	nodes, err := nl.List(ctx, cfg.Namespace)
	if err != nil {
		return fmt.Errorf("listing nodes failed: %w", err)
	}

	nodes, omitted := filterBeeNodes(nodes)

	if len(omitted) > 0 {
		opts.log.Infof("ignoring pods %v", omitted)
	}

	stakeAllNodes(ctx, nodes, cfg.MinAmounts.SwarmToken, opts.log)

	return nil
}

func stakeAllNodes(ctx context.Context, nodes []NodeInfo, minValue float64, log logging.Logger) {
	wg := sync.WaitGroup{}
	wg.Add(len(nodes))

	var skipped, staked atomic.Int32

	for _, n := range nodes {
		go func(node NodeInfo) {
			defer wg.Done()

			si, err := fetchStakeInfo(ctx, node.Address)
			if err != nil {
				log.Infof("get stake info for node[%s] failed; reason: %s", node.Name, err)
				return
			}

			amount := calcTopUpAmount(minValue, si.StakedAmount, wallet.SwarmTokenDecimals)
			if amount.Cmp(big.NewInt(0)) <= 0 {
				skipped.Add(1)
				log.Infof("node[%s] - already staked", node.Name)
				// Top up is not needed, current stake value is sufficient
				return
			}

			if err = stakeNode(ctx, node.Address, amount); err != nil {
				log.Infof("node[%s] - staking failed; reason: %s", node.Name, err)
			} else {
				log.Infof("node[%s] - staked", node.Name)
				staked.Add(1)
			}
		}(n)
	}

	wg.Wait()

	log.Infof("staked %d", staked.Load())
	log.Infof("skipped %d", skipped.Load())
	log.Infof("failed %d", len(nodes)-int(staked.Load())-int(skipped.Load()))
	log.Infof("total %d", len(nodes))
}

func stakeNode(ctx context.Context, nodeAddress string, amount *big.Int) error {
	_, err := sendHTTPRequest(ctx, http.MethodPost, nodeAddress+"/stake/"+amount.String())

	return err
}

type stakeInfo struct {
	StakedAmount *big.Int
}

func fetchStakeInfo(ctx context.Context, nodeAddress string) (stakeInfo, error) {
	responseBytes, err := sendHTTPRequest(ctx, http.MethodGet, nodeAddress+"/stake")
	if err != nil {
		return stakeInfo{}, fmt.Errorf("get bee stake info failed: %w", err)
	}

	response := struct {
		StakedAmount *bigint.BigInt `json:"stakedAmount"`
	}{}
	if err := json.Unmarshal(responseBytes, &response); err != nil {
		return stakeInfo{}, fmt.Errorf("failed to unmarshal response :%w", err)
	}

	return stakeInfo{
		StakedAmount: response.StakedAmount.Int,
	}, nil
}

func filterBeeNodes(nodes []NodeInfo) ([]NodeInfo, []NodeInfo) {
	result := make([]NodeInfo, 0, len(nodes))
	omitted := make([]NodeInfo, 0)

	for _, n := range nodes {
		parts := strings.Split(n.Name, "-")
		if slices.Contains(parts, "bee") {
			result = append(result, n)
		} else {
			omitted = append(omitted, n)
		}
	}

	return result, omitted
}
