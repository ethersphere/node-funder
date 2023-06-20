// Copyright 2023 The Swarm Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package funder

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"math/big"
	"net/http"
	"strings"
	"sync"

	"github.com/ethersphere/bee/pkg/bigint"
	"github.com/ethersphere/node-funder/pkg/wallet"
	"k8s.io/utils/strings/slices"
)

func Stake(ctx context.Context, cfg Config, nl NodeLister) error {
	log.Printf("node staking started...")
	defer log.Print("node staking finished")

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
		log.Printf("ignoring pods %v", omitted)
	}

	stakeAllNodes(ctx, nodes, cfg.MinAmounts.SwarmToken)

	return nil
}

func stakeAllNodes(ctx context.Context, nodes []NodeInfo, min float64) {
	wg := sync.WaitGroup{}
	wg.Add(len(nodes))

	for _, n := range nodes {
		go func(node NodeInfo) {
			defer wg.Done()

			si, err := fetchStakeInfo(ctx, node.Address)
			if err != nil {
				log.Printf("get stake info for node[%s] failed; reason: %s", node.Name, err)
				return
			}

			amount := calcTopUpAmount(min, si.StakedAmount, wallet.SwarmTokenDecimals)
			if amount.Cmp(big.NewInt(0)) <= 0 {
				log.Printf("node[%s] - already staked", node.Name)
				// Top up is not needed, current stake value is sufficient
				return
			}

			if err = stakeNode(ctx, node.Address, amount); err != nil {
				log.Printf("node[%s] - staking failed; reason: %s", node.Name, err)
			} else {
				log.Printf("node[%s] - staked", node.Name)
			}
		}(n)
	}

	wg.Wait()
}

func stakeNode(ctx context.Context, nodeAddress string, amount *big.Int) error {
	_, err := sendHTTPRequest(ctx, http.MethodPost, nodeAddress+"/stake/"+formatAmount(amount, 16))
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
