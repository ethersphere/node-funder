// Copyright 2022 The Swarm Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package main

import (
	"context"
	"fmt"
	"log"

	"github.com/ethereum/go-ethereum/ethclient"
	"github.com/ethereum/go-ethereum/rpc"
	"github.com/ethersphere/node-funder/pkg/funder"
	"github.com/ethersphere/node-funder/pkg/wallet"
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

	fundingWallet, err := makeFundingWallet(ctx, cfg)
	if err != nil {
		log.Fatalf("could not make funding wallet: %v", err)
	}

	if err = funder.Fund(ctx, cfg, nl, fundingWallet); err != nil {
		log.Fatalf("error while funding: %v", err)
	}
}

func makeFundingWallet(ctx context.Context, cfg funder.Config) (*wallet.Wallet, error) {
	key, err := makeWalletKey(cfg)
	if err != nil {
		return nil, fmt.Errorf("making wallet key failed: %w", err)
	}

	_, err = key.PublicAddress()
	if err != nil {
		return nil, fmt.Errorf("getting wallet public key failed: %w", err)
	}

	ethClient, err := makeEthClient(ctx, cfg.ChainNodeEndpoint)
	if err != nil {
		return nil, fmt.Errorf("making eth client failed: %w", err)
	}

	fundingWallet := wallet.New(ethClient, key)

	return fundingWallet, nil
}

func makeWalletKey(cfg funder.Config) (wallet.Key, error) {
	if cfg.WalletKey == "" {
		return wallet.GenerateKey()
	}

	return wallet.Key(cfg.WalletKey), nil
}

func makeEthClient(ctx context.Context, endpoint string) (*ethclient.Client, error) {
	rpcClient, err := rpc.DialContext(ctx, endpoint)
	if err != nil {
		return nil, err
	}

	return ethclient.NewClient(rpcClient), nil
}
