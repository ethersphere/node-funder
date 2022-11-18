// Copyright 2022 The Swarm Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package wallet

import (
	"context"
	"crypto/ecdsa"
	"errors"
	"fmt"
	"math/big"
	"strings"

	"github.com/ethereum/go-ethereum/accounts/abi"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/ethereum/go-ethereum/ethclient"
	"github.com/ethersphere/go-sw3-abi/sw3abi"
)

var erc20ABI = mustParseABI(sw3abi.ERC20ABIv0_3_1)

const gasLimit = uint64(21000)

type Wallet struct {
	client *ethclient.Client
	key    WalletKey
}

func New(client *ethclient.Client, key WalletKey) *Wallet {
	return &Wallet{
		client: client,
		key:    key,
	}
}

func (w *Wallet) CainID(ctx context.Context) (int64, error) {
	id, err := w.client.NetworkID(ctx)
	if err != nil {
		return 0, err
	}

	return id.Int64(), nil
}

func (w *Wallet) TransferNative(ctx context.Context, cid int64, toAddr common.Address, amount *big.Int) error {
	err := w.sendTransaction(ctx, cid, toAddr, amount, nil)
	if err != nil {
		return fmt.Errorf("failed to make native coin transfer, %w", err)
	}

	return nil
}

func (w *Wallet) TransferERC20(ctx context.Context, cid int64, toAddr common.Address, amount *big.Int, token Token) error {
	callData, err := erc20ABI.Pack("transfer", toAddr, amount)
	if err != nil {
		return fmt.Errorf("failed to pack abi, %w", err)
	}

	err = w.sendTransaction(ctx, cid, token.Contract, big.NewInt(0), callData)
	if err != nil {
		return fmt.Errorf("failed to make ERC20 token transfer, %w", err)
	}

	return nil
}

func (w *Wallet) sendTransaction(ctx context.Context, cid int64, toAddr common.Address, amount *big.Int, callData []byte) error {
	chainID, err := w.client.NetworkID(ctx)
	if err != nil {
		return fmt.Errorf("failed to get network id, %w", err)
	}

	if chainID.Int64() != cid {
		return errors.New("wallet chain id does not match chain id for transfer")
	}

	privateKey, publicKey, err := w.keys()
	if err != nil {
		return fmt.Errorf("failed to get wallet keys, %w", err)
	}

	fromAddress := crypto.PubkeyToAddress(*publicKey)

	nonce, err := w.client.PendingNonceAt(ctx, fromAddress)
	if err != nil {
		return fmt.Errorf("failed to make nonce, %w", err)
	}

	gasPrice, err := w.client.SuggestGasPrice(ctx)
	if err != nil {
		return fmt.Errorf("failed to get suggested gas price, %w", err)
	}

	tx := types.NewTransaction(nonce, toAddr, amount, gasLimit, gasPrice, callData)

	signedTx, err := types.SignTx(tx, types.NewEIP155Signer(chainID), privateKey)
	if err != nil {
		return fmt.Errorf("failed to sign transaction, %w", err)
	}

	err = w.client.SendTransaction(ctx, signedTx)
	if err != nil {
		return fmt.Errorf("failed to send transaction, %w", err)
	}

	return nil
}

func (w *Wallet) keys() (*ecdsa.PrivateKey, *ecdsa.PublicKey, error) {
	privateKey, err := crypto.HexToECDSA(string(w.key))
	if err != nil {
		return nil, nil, err
	}

	publicKeyECDSA, ok := privateKey.Public().(*ecdsa.PublicKey)
	if !ok {
		return nil, nil, fmt.Errorf("failed to get public key from private key")
	}

	return privateKey, publicKeyECDSA, nil
}

func mustParseABI(json string) abi.ABI {
	cabi, err := abi.JSON(strings.NewReader(json))
	if err != nil {
		panic(fmt.Sprintf("error creating ABI for contract: %v", err))
	}

	return cabi
}