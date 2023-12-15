// Copyright 2022 The Swarm Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package main

import (
	"context"
	"log"

	"github.com/ethersphere/node-funder/pkg/funder"
	"github.com/spf13/cobra"
)

func main() {
	cfg := funder.Config{}

	rootCmd := &cobra.Command{
		Use: "funder",
		Run: func(cmd *cobra.Command, args []string) {
			if err := cmd.Help(); err != nil {
				log.Fatal(err)
			}
		},
	}

	fundCmd := &cobra.Command{
		Use:   "fund",
		Short: "fund (top up) bee node wallets",
		Run: func(cmd *cobra.Command, args []string) {
			doFund(cfg)
		},
	}

	fundCmd.PersistentFlags().StringVar(&cfg.Namespace, "namespace", "", "kubernetes namespace")
	fundCmd.PersistentFlags().StringSliceVar(&cfg.Addresses, "addresses", nil, "wallet addresses")
	fundCmd.PersistentFlags().StringVar(&cfg.ChainNodeEndpoint, "chainNodeEndpoint", "", "endpoint to chain node")
	fundCmd.PersistentFlags().StringVar(&cfg.WalletKey, "walletKey", "", "wallet key")
	fundCmd.PersistentFlags().Float64Var(&cfg.MinAmounts.NativeCoin, "minNative", 0, "specifies min amount of chain native coins (DAI) nodes should have")
	fundCmd.PersistentFlags().Float64Var(&cfg.MinAmounts.SwarmToken, "minSwarm", 0, "specifies min amount of swarm tokens (BZZ) nodes should have")

	stakeCmd := &cobra.Command{
		Use:   "stake",
		Short: "stake (top up) bee nodes",
		Run: func(cmd *cobra.Command, args []string) {
			doStake(cfg)
		},
	}
	stakeCmd.PersistentFlags().StringVar(&cfg.Namespace, "namespace", "", "kubernetes namespace")
	stakeCmd.PersistentFlags().Float64Var(&cfg.MinAmounts.SwarmToken, "minSwarm", 0, "specifies min amount of swarm tokens (BZZ) nodes should have staked")

	rootCmd.AddCommand(fundCmd)
	rootCmd.AddCommand(stakeCmd)

	if err := rootCmd.Execute(); err != nil {
		log.Fatal(err)
	}
}

func doFund(cfg funder.Config) {
	ctx := context.Background()

	if cfg.Namespace == "" && len(cfg.Addresses) == 0 {
		log.Fatalf("--namespace or --addresses must be set")
		return
	}

	if cfg.ChainNodeEndpoint == "" {
		log.Fatalf("--chainNodeEndpoint must be set")
		return
	}

	if cfg.WalletKey == "" {
		log.Fatalf("--walletKey must be set")
		return
	}

	if err := funder.Fund(ctx, cfg, nil, nil); err != nil {
		log.Fatalf("error while funding: %v", err)
	}
}

func doStake(cfg funder.Config) {
	ctx := context.Background()

	if cfg.Namespace == "" {
		log.Fatalf("--namespace must be set")
		return
	}

	if err := funder.Stake(ctx, cfg, nil); err != nil {
		log.Fatalf("error while funding: %v", err)
	}
}
