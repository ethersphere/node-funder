// Copyright 2022 The Swarm Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package main

import (
	"context"
	"fmt"
	"io"
	"log"
	"strings"

	"github.com/ethersphere/beekeeper/pkg/logging"
	"github.com/ethersphere/node-funder/pkg/funder"
	"github.com/spf13/cobra"
)

const (
	optionLogVerbosity string = "log-verbosity"
)

func main() {
	cfg := funder.Config{}

	var logLevel string

	rootCmd := &cobra.Command{
		Use: "funder",
		Run: func(cmd *cobra.Command, args []string) {
			if err := cmd.Help(); err != nil {
				log.Fatal(err)
			}
		},
	}

	rootCmd.PersistentFlags().StringVar(&logLevel, optionLogVerbosity, "info", "log verbosity level 0=silent, 1=error, 2=warn, 3=info, 4=debug, 5=trace")

	logger, err := newLogger(rootCmd, logLevel)
	if err != nil {
		log.Fatal(err)
	}

	fundCmd := &cobra.Command{
		Use:   "fund",
		Short: "fund (top up) bee node wallets",
		Run: func(cmd *cobra.Command, args []string) {
			doFund(cfg, logger)
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
			doStake(cfg, logger)
		},
	}
	stakeCmd.PersistentFlags().StringVar(&cfg.Namespace, "namespace", "", "kubernetes namespace")
	stakeCmd.PersistentFlags().Float64Var(&cfg.MinAmounts.SwarmToken, "minSwarm", 0, "specifies min amount of swarm tokens (BZZ) nodes should have staked")

	rootCmd.AddCommand(fundCmd, stakeCmd)

	if err := rootCmd.Execute(); err != nil {
		logger.Fatal(err)
	}
}

func doFund(cfg funder.Config, logger logging.Logger) {
	ctx := context.Background()

	if cfg.Namespace == "" && len(cfg.Addresses) == 0 {
		logger.Fatalf("--namespace or --addresses must be set")
		return
	}

	if cfg.ChainNodeEndpoint == "" {
		logger.Fatalf("--chainNodeEndpoint must be set")
		return
	}

	if cfg.WalletKey == "" {
		logger.Fatalf("--walletKey must be set")
		return
	}

	if err := funder.Fund(ctx, cfg, nil, nil); err != nil {
		logger.Fatalf("error while funding: %v", err)
	}
}

func doStake(cfg funder.Config, logger logging.Logger) {
	ctx := context.Background()

	if cfg.Namespace == "" {
		logger.Fatalf("--namespace must be set")
		return
	}

	if err := funder.Stake(ctx, cfg, nil); err != nil {
		logger.Fatalf("error while funding: %v", err)
	}
}

func newLogger(cmd *cobra.Command, verbosity string) (logging.Logger, error) {
	var logger logging.Logger

	switch strings.ToLower(verbosity) {
	case "0", "silent":
		logger = logging.New(io.Discard, 0)
	case "1", "error":
		logger = logging.New(cmd.OutOrStdout(), 2)
	case "2", "warn":
		logger = logging.New(cmd.OutOrStdout(), 3)
	case "3", "info":
		logger = logging.New(cmd.OutOrStdout(), 4)
	case "4", "debug":
		logger = logging.New(cmd.OutOrStdout(), 5)
	case "5", "trace":
		logger = logging.New(cmd.OutOrStdout(), 6)
	default:
		return nil, fmt.Errorf("unknown %s level %q, use help to check flag usage options", optionLogVerbosity, verbosity)
	}

	return logger, nil
}
