// Copyright 2022 The Swarm Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package wallet

import (
	"context"
	"encoding/hex"
	"fmt"
	"math/big"
	"strings"

	"github.com/ethereum/go-ethereum"
	"github.com/ethereum/go-ethereum/accounts/abi"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethersphere/go-sw3-abi/sw3abi"
)

var erc20ABI = mustParseABI(sw3abi.ERC20ABIv0_6_5)

type TokenWallet interface {
	Balance(
		ctx context.Context,
		addr common.Address,
		token Token,
	) (*big.Int, error)

	Transfer(
		ctx context.Context,
		toAddr common.Address,
		amount *big.Int,
		token Token,
	) error
}

type Wallet struct {
	key    Key
	client BackendClient
	native TokenWallet
	erc20  TokenWallet
}

func New(client BackendClient, key Key) *Wallet {
	trxSender := newTransactionSender(client, key)

	return &Wallet{
		key:    key,
		client: client,
		native: newNativeWallet(client, trxSender),
		erc20:  newERC20Wallet(client, trxSender),
	}
}

func (w *Wallet) PublicAddress() common.Address {
	addr, _ := w.key.PublicAddress()
	return addr
}

func (w *Wallet) ChainID(ctx context.Context) (int64, error) {
	id, err := w.client.ChainID(ctx)
	if err != nil {
		return 0, fmt.Errorf("failed to get network id, %w", err)
	}

	return id.Int64(), nil
}

func (w *Wallet) Native() TokenWallet {
	return w.native
}

func (w *Wallet) ERC20() TokenWallet {
	return w.erc20
}

func (w *Wallet) BalanceNative(
	ctx context.Context,
	addr common.Address,
) (*big.Int, error) {
	return w.native.Balance(ctx, addr, Token{})
}

func (w *Wallet) TransferNative(
	ctx context.Context,
	toAddr common.Address,
	amount *big.Int,
) error {
	return w.native.Transfer(ctx, toAddr, amount, Token{})
}

func (w *Wallet) BalanceERC20(
	ctx context.Context,
	addr common.Address,
	token Token,
) (*big.Int, error) {
	return w.erc20.Balance(ctx, addr, token)
}

func (w *Wallet) TransferERC20(
	ctx context.Context,
	toAddr common.Address,
	amount *big.Int,
	token Token,
) error {
	return w.erc20.Transfer(ctx, toAddr, amount, token)
}

type nativeWallet struct {
	client    BackendClient
	trxSender TransactionSender
}

func newNativeWallet(
	client BackendClient,
	trxSender TransactionSender,
) *nativeWallet {
	return &nativeWallet{
		client:    client,
		trxSender: trxSender,
	}
}

func (w *nativeWallet) Transfer(
	ctx context.Context,
	toAddr common.Address,
	amount *big.Int,
	token Token,
) error {
	err := w.trxSender.Send(ctx, toAddr, amount, nil)
	if err != nil {
		return fmt.Errorf("failed to make native token transfer, %w", err)
	}

	return nil
}

func (w *nativeWallet) Balance(
	ctx context.Context,
	addr common.Address,
	token Token,
) (*big.Int, error) {
	return w.client.BalanceAt(ctx, addr, nil)
}

type erc20Wallet struct {
	client    BackendClient
	trxSender TransactionSender
}

func newERC20Wallet(
	client BackendClient,
	trxSender TransactionSender,
) *erc20Wallet {
	return &erc20Wallet{
		client:    client,
		trxSender: trxSender,
	}
}

func (w *erc20Wallet) Balance(
	ctx context.Context,
	addr common.Address,
	token Token,
) (*big.Int, error) {
	callData, err := erc20ABI.Pack("balanceOf", addr)
	if err != nil {
		return nil, fmt.Errorf("failed to pack abi, %w", err)
	}

	resp, err := w.client.CallContract(ctx, ethereum.CallMsg{
		To:   &token.Contract,
		Data: callData,
	}, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to call contract, %w", err)
	}

	if len(resp) == 0 {
		return nil, fmt.Errorf("empty response from contract call: contract=%s", token.Contract.Hex())
	}

	var balance *big.Int

	if err = erc20ABI.UnpackIntoInterface(&balance, "balanceOf", resp); err != nil {
		return nil, fmt.Errorf("failed to unpack abi, %w", err)
	}

	return balance, nil
}

func (w *erc20Wallet) Transfer(
	ctx context.Context,
	toAddr common.Address,
	amount *big.Int,
	token Token,
) error {
	chainID, err := w.client.ChainID(ctx)
	if err != nil {
		return fmt.Errorf("failed to get network id, %w", err)
	}

	callData, err := erc20ABI.Pack("transfer", toAddr, amount)
	if err != nil {
		return fmt.Errorf("failed to pack abi, %w", err)
	}

	// Custom handling for LocalnetChainID.
	if chainID.Int64() == LocalnetChainID {
		mint, decodeErr := hex.DecodeString("40c10f19") // mint(address,uint256)
		if decodeErr != nil {
			return fmt.Errorf("failed decode string %w", err)
		}
		// Replace the first 4 bytes of the call data (transfer) with the localnet mint function.
		copy(callData[:4], mint)
	}

	if err = w.trxSender.Send(ctx, token.Contract, nil, callData); err != nil {
		return fmt.Errorf("failed to make ERC20 token transfer, %w", err)
	}

	return nil
}

func mustParseABI(json string) abi.ABI {
	cabi, err := abi.JSON(strings.NewReader(json))
	if err != nil {
		panic(fmt.Sprintf("error creating ABI for contract: %v", err))
	}

	return cabi
}
