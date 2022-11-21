// Copyright 2022 The Swarm Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package funder

import (
	"context"
	"errors"
	"fmt"
	"log"
	"math/big"
	"strings"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/ethclient"
	"github.com/ethereum/go-ethereum/rpc"
	"github.com/ethersphere/node-funder/pkg/kube"
	"github.com/ethersphere/node-funder/pkg/wallet"
)

func FundAllNodes(cfg Config) error {
	log.Printf("funding nodes using config: %+v", cfg)

	ctx := context.Background()

	key, err := makeWalletKey(cfg)
	if err != nil {
		return fmt.Errorf("failed getting wallet key: %w", err)
	}

	log.Printf("using wallet address: %s", key)

	ethClient, err := makeEthClient(ctx, cfg.ChainNodeEndpoint)
	if err != nil {
		return fmt.Errorf("failed make eth client: %w", err)
	}

	fundingWallet := wallet.New(ethClient, key)

	kubeClient, err := kube.NewKube()
	if err != nil {
		return fmt.Errorf("connecting kube client with error: %w", err)
	}

	namespace, err := kube.FetchNamespaceNodeInfo(ctx, kubeClient, cfg.Namespace)
	if err != nil {
		return fmt.Errorf("get node info failed with error: %w", err)
	}

	fundNodeRespC := make(chan fundNodeResp, len(namespace.Nodes))
	for _, n := range namespace.Nodes {
		go fundNode(ctx, fundingWallet, cfg.MinAmounts, n, fundNodeRespC)
	}

	for i := 0; i < len(namespace.Nodes); i++ {
		resp := <-fundNodeRespC
		if resp.err != nil {
			log.Printf("failed to fund node (%s): %s", resp.node.Name, resp.err)
		} else {
			log.Printf("node funded (%s)", resp.node.Name)
		}
	}

	return nil
}

func makeWalletKey(cfg Config) (wallet.WalletKey, error) {
	if cfg.WalletKey == "" {
		return wallet.GenerateKey()
	}

	return wallet.WalletKey(cfg.WalletKey), nil
}

func makeEthClient(ctx context.Context, endpoint string) (*ethclient.Client, error) {
	rpcClient, err := rpc.DialContext(ctx, endpoint)
	if err != nil {
		return nil, err
	}

	return ethclient.NewClient(rpcClient), nil
}

type fundNodeResp struct {
	node kube.Node
	err  error
}

var (
	ErrFailedFundingNode               = errors.New("failed funding node")
	ErrFailedFudningNodeWithSwarmToken = errors.New("failed funding node with swarm token")
	ErrFailedFudningNodeWithNativeCoin = errors.New("failed funding node with native coin")
)

func fundNode(
	ctx context.Context,
	fundingWallet *wallet.Wallet,
	minAmounts MinAmounts,
	node kube.Node,
	fundNodeRespC chan<- fundNodeResp,
) {
	fundRespC := make(chan error, 2)

	go fundNodeNativeCoin(ctx, fundingWallet, minAmounts, node, fundRespC)
	go fundNodeSwarmToken(ctx, fundingWallet, minAmounts, node, fundRespC)

	var errorMsg []string

	for i := 0; i < 2; i++ {
		if err := <-fundRespC; err != nil {
			errorMsg = append(errorMsg, err.Error())
		}
	}

	var err error
	if len(errorMsg) > 0 {
		err = fmt.Errorf("%w (%s), reason: %s", ErrFailedFundingNode, node.Name, strings.Join(errorMsg, ", "))
	}

	fundNodeRespC <- fundNodeResp{
		node: node,
		err:  err,
	}
}

func fundNodeNativeCoin(
	ctx context.Context,
	fundingWallet *wallet.Wallet,
	minAmounts MinAmounts,
	node kube.Node,
	respC chan<- error,
) {
	respC <- func() error {
		cid := node.WalletInfo.ChainID

		token, err := wallet.NativeCoinForChain(cid)
		if err != nil {
			return fmt.Errorf("%w: %s", ErrFailedFudningNodeWithNativeCoin, err)
		}

		topUpAmount := CalcTopUpAmount(minAmounts.NativeCoin, node.WalletInfo.NativeCoin, token.Decimals)
		if topUpAmount.Cmp(big.NewInt(0)) <= 0 {
			// Node has enough in wallet, top up is not needed
			return nil
		}

		address := common.HexToAddress(node.WalletInfo.Address)

		err = fundingWallet.TransferNative(ctx, cid, address, topUpAmount)
		if err != nil {
			return fmt.Errorf("%w: %s", ErrFailedFudningNodeWithNativeCoin, err)
		}

		return nil
	}()
}

func fundNodeSwarmToken(
	ctx context.Context,
	fundingWallet *wallet.Wallet,
	minAmounts MinAmounts,
	node kube.Node,
	respC chan<- error,
) {
	respC <- func() error {
		cid := node.WalletInfo.ChainID

		token, err := wallet.SwarmTokenForChain(cid)
		if err != nil {
			return fmt.Errorf("%w: %s", ErrFailedFudningNodeWithSwarmToken, err)
		}

		topUpAmount := CalcTopUpAmount(minAmounts.SwarmToken, node.WalletInfo.SwarmToken, token.Decimals)
		if topUpAmount.Cmp(big.NewInt(0)) <= 0 {
			// Node has enough in wallet, top up is not needed
			return nil
		}

		address := common.HexToAddress(node.WalletInfo.Address)

		err = fundingWallet.TransferERC20(ctx, cid, address, topUpAmount, token)
		if err != nil {
			return fmt.Errorf("%w: %s", ErrFailedFudningNodeWithSwarmToken, err)
		}

		return nil
	}()
}

func CalcTopUpAmount(min float64, nodeAmount *big.Int, tokenDecimals int) *big.Int {
	exp := big.NewInt(0).Exp(big.NewInt(10), big.NewInt(int64(tokenDecimals)), nil)

	minAmount := big.NewFloat(min)
	minAmount = minAmount.Mul(
		minAmount,
		big.NewFloat(0).SetInt(exp),
	)

	minAmountInt, _ := minAmount.Int(big.NewInt(0))

	return minAmountInt.Sub(minAmountInt, nodeAmount)
}
