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
	"github.com/ethersphere/node-funder/pkg/types"
	"github.com/ethersphere/node-funder/pkg/wallet"
)

func Fund(cfg Config) error {
	if cfg.Namespace != "" {
		return FundNamespace(cfg)
	}

	return FundAddresses(cfg)
}

func FundNamespace(cfg Config) error {
	log.Printf("node funder started...")
	defer log.Print("node funder finished")

	ctx := context.Background()

	fundingWallet, err := makeFundingWallet(ctx, cfg)
	if err != nil {
		return fmt.Errorf("failed to make funding wallet: %w", err)
	}

	kubeClient, err := kube.NewKube()
	if err != nil {
		return fmt.Errorf("connecting kube client failed: %w", err)
	}

	log.Printf("fetchin nodes for namespace=%s", cfg.Namespace)

	namespace, err := kube.FetchNamespaceNodeInfo(ctx, kubeClient, cfg.Namespace)
	if err != nil {
		return fmt.Errorf("fetching namespace nodes failed: %w", err)
	}

	log.Printf("funding nodes (count=%d) up to amounts=%+v", len(namespace.NodeWallets), cfg.MinAmounts)

	if ok := fundAllWallets(ctx, fundingWallet, cfg.MinAmounts, namespace.NodeWallets); !ok {
		return fmt.Errorf("funding all nodes failed")
	}

	return nil
}

func FundAddresses(cfg Config) error {
	log.Printf("node funder started...")
	defer log.Print("node funder finished")

	ctx := context.Background()

	fundingWallet, err := makeFundingWallet(ctx, cfg)
	if err != nil {
		return fmt.Errorf("failed to make funding wallet: %w", err)
	}

	cid, err := fundingWallet.CainID(ctx)
	if err != nil {
		return err
	}

	wallets := makeWalletInfoFromAddresses(cfg.Addresses, cid)

	log.Printf("funding wallets (count=%d) up to amounts=%+v", len(wallets), cfg.MinAmounts)

	if ok := fundAllWallets(ctx, fundingWallet, cfg.MinAmounts, wallets); !ok {
		return fmt.Errorf("funding all wallets failed")
	}

	return nil
}

func makeWalletInfoFromAddresses(addrs []string, cid int64) []types.WalletInfo {
	result := make([]types.WalletInfo, 0, len(addrs))
	for _, addr := range addrs {
		result = append(result, types.WalletInfo{
			Name:    fmt.Sprintf("wallet (address=%s)", addr),
			ChainID: cid,
			Address: addr,
		})
	}

	return result
}

func fundAllWallets(
	ctx context.Context,
	fundingWallet *wallet.Wallet,
	minAmounts MinAmounts,
	wallets []types.WalletInfo,
) bool {
	fundWalletRespC := make([]<-chan fundWalletResp, len(wallets))
	for i, wi := range wallets {
		fundWalletRespC[i] = fundWalletAsync(ctx, fundingWallet, minAmounts, wi)
	}

	allWalletsFunded := true

	for _, respC := range fundWalletRespC {
		resp := <-respC
		name := resp.wallet.Name
		cid := resp.wallet.ChainID

		if resp.err != nil {
			log.Printf("%s funding failed - error: %s", name, resp.err)

			allWalletsFunded = false

			continue
		}

		if resp.transferredNativeAmount == nil && resp.transferredSwarmAmount == nil {
			log.Printf("%s funded - already funded", name)
		} else {
			token, _ := wallet.NativeCoinForChain(cid)
			nativeAmount := formatAmount(resp.transferredNativeAmount, token.Decimals)
			token, _ = wallet.SwarmTokenForChain(cid)
			swarmAmount := formatAmount(resp.transferredSwarmAmount, token.Decimals)

			log.Printf("%s funded - transferred { native: %s, swarm: %s }", name, nativeAmount, swarmAmount)
		}
	}

	return allWalletsFunded
}

func makeFundingWallet(ctx context.Context, cfg Config) (*wallet.Wallet, error) {
	key, err := makeWalletKey(cfg)
	if err != nil {
		return nil, fmt.Errorf("making wallet key failed: %w", err)
	}

	pubKeyAddr, err := key.PublicAddress()
	if err != nil {
		return nil, fmt.Errorf("getting wallet public key failed: %w", err)
	}

	log.Printf("using wallet address (public key address): %s", pubKeyAddr)

	ethClient, err := makeEthClient(ctx, cfg.ChainNodeEndpoint)
	if err != nil {
		return nil, fmt.Errorf("making eth client failed: %w", err)
	}

	fundingWallet := wallet.New(ethClient, key)

	return fundingWallet, nil
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

type fundWalletResp struct {
	wallet                  types.WalletInfo
	err                     error
	transferredNativeAmount *big.Int
	transferredSwarmAmount  *big.Int
}

var (
	ErrFailedFunding                = errors.New("failed funding")
	ErrFailedFudningWithSwarmToken  = errors.New("failed funding with swarm token")
	ErrFailedFudningWithNativeToken = errors.New("failed funding with native token")
)

func fundWalletAsync(
	ctx context.Context,
	fundingWallet *wallet.Wallet,
	minAmounts MinAmounts,
	wi types.WalletInfo,
) <-chan fundWalletResp {
	respC := make(chan fundWalletResp, 1)

	go func() {
		if err := validateChainID(ctx, fundingWallet, wi); err != nil {
			respC <- fundWalletResp{wallet: wi, err: err}
			return
		}

		nativeResp := <-topUpWalletAsync(ctx, wallet.NativeCoinForChain, fundingWallet.Native(), minAmounts.NativeCoin, wi)
		swarmResp := <-topUpWalletAsync(ctx, wallet.SwarmTokenForChain, fundingWallet.ERC20(), minAmounts.SwarmToken, wi)

		err := mergeErrors(
			ErrFailedFunding,
			mergeErrors(ErrFailedFudningWithNativeToken, nativeResp.err),
			mergeErrors(ErrFailedFudningWithSwarmToken, swarmResp.err),
		)

		respC <- fundWalletResp{
			wallet:                  wi,
			err:                     err,
			transferredNativeAmount: nativeResp.transferredAmount,
			transferredSwarmAmount:  swarmResp.transferredAmount,
		}
	}()

	return respC
}

func validateChainID(ctx context.Context, fundingWallet *wallet.Wallet, wi types.WalletInfo) error {
	if cid, err := fundingWallet.CainID(ctx); err != nil {
		return fmt.Errorf("failed getting funding wallet's chain ID: %w", err)
	} else if cid != wi.ChainID {
		return fmt.Errorf("wallet info chain ID (%d) does not match funding wallet chain ID (%d)", wi.ChainID, cid)
	}

	return nil
}

func mergeErrors(main error, errs ...error) error {
	var errorMsg []string

	for _, err := range errs {
		if err != nil {
			errorMsg = append(errorMsg, err.Error())
		}
	}

	if len(errorMsg) > 0 {
		return fmt.Errorf("%w, reason: %s", ErrFailedFunding, strings.Join(errorMsg, ", "))
	}

	return nil
}

type transferResp struct {
	err               error
	transferredAmount *big.Int
}

func topUpWalletAsync(
	ctx context.Context,
	tokenInfoGetter wallet.TokenInfoGetterFn,
	fundingWallet wallet.TokenWallet,
	minAmount float64,
	wi types.WalletInfo,
) <-chan transferResp {
	respC := make(chan transferResp, 1)

	go func() {
		transferredAmount, err := topUpWallet(ctx, tokenInfoGetter, fundingWallet, minAmount, wi)

		respC <- transferResp{
			transferredAmount: transferredAmount,
			err:               err,
		}
	}()

	return respC
}

func topUpWallet(
	ctx context.Context,
	tokenInfoGetter wallet.TokenInfoGetterFn,
	fundingWallet wallet.TokenWallet,
	minAmount float64,
	wi types.WalletInfo,
) (*big.Int, error) {
	token, err := tokenInfoGetter(wi.ChainID)
	if err != nil {
		return nil, err
	}

	if !common.IsHexAddress(wi.Address) {
		return nil, fmt.Errorf("unexpected wallet address")
	}

	address := common.HexToAddress(wi.Address)

	currentBalance, err := fundingWallet.Balance(ctx, address, token)
	if err != nil {
		return nil, err
	}

	topUpAmount := CalcTopUpAmount(minAmount, currentBalance, token.Decimals)
	if topUpAmount.Cmp(big.NewInt(0)) <= 0 {
		// Top up is not needed, current balance is sufficient
		return nil, nil
	}

	err = fundingWallet.Transfer(ctx, address, topUpAmount, token)
	if err != nil {
		return nil, err
	}

	return topUpAmount, nil
}

func CalcTopUpAmount(min float64, currAmount *big.Int, decimals int) *big.Int {
	exp := big.NewInt(0).Exp(big.NewInt(10), big.NewInt(int64(decimals)), nil)

	minAmount := big.NewFloat(min)
	minAmount = minAmount.Mul(
		minAmount,
		big.NewFloat(0).SetInt(exp),
	)

	minAmountInt, _ := minAmount.Int(big.NewInt(0))

	return minAmountInt.Sub(minAmountInt, currAmount)
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
