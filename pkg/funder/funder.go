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

	pubKeyAddr, err := key.PublicAddress()
	if err != nil {
		return fmt.Errorf("failed getting wallet public key: %w", err)
	}

	log.Printf("using wallet address (public key address): %s", pubKeyAddr)

	ethClient, err := makeEthClient(ctx, cfg.ChainNodeEndpoint)
	if err != nil {
		return fmt.Errorf("failed make eth client: %w", err)
	}

	fundingWallet := wallet.New(ethClient, key)

	kubeClient, err := kube.NewKube()
	if err != nil {
		return fmt.Errorf("connecting kube client with error: %w", err)
	}

	log.Printf("fetchin nodes for namespace=%s", cfg.Namespace)

	namespace, err := kube.FetchNamespaceNodeInfo(ctx, kubeClient, cfg.Namespace)
	if err != nil {
		return fmt.Errorf("get node info failed with error: %w", err)
	}

	log.Printf("funding nodes... (count=%d)", len(namespace.Nodes))

	fundNodeRespC := make(chan fundNodeResp, len(namespace.Nodes))
	for _, n := range namespace.Nodes {
		go fundNode(ctx, fundingWallet, cfg.MinAmounts, n, fundNodeRespC)
	}

	for i := 0; i < len(namespace.Nodes); i++ {
		resp := <-fundNodeRespC
		name := resp.node.Name
		walletAddr := resp.node.WalletInfo.Address

		if resp.err != nil {
			log.Printf("failed to fund node (%s) (wallet=%s) - error: %s", name, walletAddr, resp.err)
			continue
		}

		if resp.transferredNativeAmount == nil && resp.transferredSwarmAmount == nil {
			log.Printf("node (%s) funded (wallet=%s) - already funded", name, walletAddr)
		} else {
			token, _ := wallet.NativeCoinForChain(resp.node.WalletInfo.ChainID)
			nativeAmount := formatAmount(resp.transferredNativeAmount, token.Decimals)
			token, _ = wallet.SwarmTokenForChain(resp.node.WalletInfo.ChainID)
			swarmAmount := formatAmount(resp.transferredSwarmAmount, token.Decimals)

			log.Printf("node (%s) funded (wallet=%s) - transferred native: %s, transferred swarm: %s ", name, walletAddr, nativeAmount, swarmAmount)
		}
	}

	return nil
}

func makeWalletKey(cfg Config) (wallet.Key, error) {
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

type fundNodeResp struct {
	node                    kube.Node
	err                     error
	transferredNativeAmount *big.Int
	transferredSwarmAmount  *big.Int
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
	transferNativeRespC := make(chan transferResp)
	transferSwarmRespC := make(chan transferResp)

	go transferFunds(transferNativeCoin, ctx, fundingWallet, minAmounts, node, transferNativeRespC)
	go transferFunds(transferSwarmToken, ctx, fundingWallet, minAmounts, node, transferSwarmRespC)

	transferNativeResp := <-transferNativeRespC
	transferSwarmResp := <-transferSwarmRespC

	fundNodeRespC <- fundNodeResp{
		node:                    node,
		err:                     mergeErrors(ErrFailedFundingNode, transferNativeResp.err, transferSwarmResp.err),
		transferredNativeAmount: transferNativeResp.transferredAmount,
		transferredSwarmAmount:  transferSwarmResp.transferredAmount,
	}
}

func mergeErrors(main error, errs ...error) error {
	var errorMsg []string

	for _, err := range errs {
		if err != nil {
			errorMsg = append(errorMsg, err.Error())
		}
	}

	if len(errorMsg) > 0 {
		return fmt.Errorf("%w, reason: %s", ErrFailedFundingNode, strings.Join(errorMsg, ", "))
	}

	return nil
}

type transferResp struct {
	err               error
	transferredAmount *big.Int
}

func transferFunds(
	transferFn transferFn,
	ctx context.Context,
	fundingWallet *wallet.Wallet,
	minAmounts MinAmounts,
	node kube.Node,
	transferRespC chan<- transferResp,
) {
	transferredAmount, err := transferFn(ctx, fundingWallet, minAmounts, node)

	transferRespC <- transferResp{
		err:               err,
		transferredAmount: transferredAmount,
	}
}

type transferFn = func(
	ctx context.Context,
	fundingWallet *wallet.Wallet,
	minAmounts MinAmounts,
	node kube.Node) (*big.Int, error)

func transferNativeCoin(
	ctx context.Context,
	fundingWallet *wallet.Wallet,
	minAmounts MinAmounts,
	node kube.Node,
) (*big.Int, error) {
	cid := node.WalletInfo.ChainID

	token, err := wallet.NativeCoinForChain(cid)
	if err != nil {
		return nil, fmt.Errorf("%w: %s", ErrFailedFudningNodeWithNativeCoin, err)
	}

	topUpAmount := CalcTopUpAmount(minAmounts.NativeCoin, node.WalletInfo.NativeCoin, token.Decimals)
	if topUpAmount.Cmp(big.NewInt(0)) <= 0 {
		// Node has enough in wallet, top up is not needed
		return nil, nil
	}

	if !common.IsHexAddress(node.WalletInfo.Address) {
		return nil, fmt.Errorf("%w: unexpected wallet address", ErrFailedFudningNodeWithNativeCoin)
	}

	address := common.HexToAddress(node.WalletInfo.Address)

	err = fundingWallet.TransferNative(ctx, cid, address, topUpAmount)
	if err != nil {
		return nil, fmt.Errorf("%w: %s", ErrFailedFudningNodeWithNativeCoin, err)
	}

	return topUpAmount, nil
}

func transferSwarmToken(
	ctx context.Context,
	fundingWallet *wallet.Wallet,
	minAmounts MinAmounts,
	node kube.Node,
) (*big.Int, error) {
	cid := node.WalletInfo.ChainID

	token, err := wallet.SwarmTokenForChain(cid)
	if err != nil {
		return nil, fmt.Errorf("%w: %s", ErrFailedFudningNodeWithSwarmToken, err)
	}

	topUpAmount := CalcTopUpAmount(minAmounts.SwarmToken, node.WalletInfo.SwarmToken, token.Decimals)
	if topUpAmount.Cmp(big.NewInt(0)) <= 0 {
		// Node has enough in wallet, top up is not needed
		return nil, nil
	}

	if !common.IsHexAddress(node.WalletInfo.Address) {
		return nil, fmt.Errorf("%w: unexpected wallet address", ErrFailedFudningNodeWithSwarmToken)
	}

	address := common.HexToAddress(node.WalletInfo.Address)

	err = fundingWallet.TransferERC20(ctx, cid, address, topUpAmount, token)
	if err != nil {
		return nil, fmt.Errorf("%w: %s", ErrFailedFudningNodeWithSwarmToken, err)
	}

	return topUpAmount, nil
}

func CalcTopUpAmount(min float64, nodeAmount *big.Int, decimals int) *big.Int {
	exp := big.NewInt(0).Exp(big.NewInt(10), big.NewInt(int64(decimals)), nil)

	minAmount := big.NewFloat(min)
	minAmount = minAmount.Mul(
		minAmount,
		big.NewFloat(0).SetInt(exp),
	)

	minAmountInt, _ := minAmount.Int(big.NewInt(0))

	return minAmountInt.Sub(minAmountInt, nodeAmount)
}

func formatAmount(amount *big.Int, decimals int) string {
	if amount == nil {
		return "0"
	}

	exp := big.NewInt(0).Exp(big.NewInt(10), big.NewInt(int64(decimals)), nil)

	a := big.NewFloat(0).SetInt(amount)
	a.Quo(a, big.NewFloat(0).SetInt(exp))

	return a.String()
}
