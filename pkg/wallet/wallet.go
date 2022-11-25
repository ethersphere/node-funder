// Copyright 2022 The Swarm Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package wallet

import (
	"context"
	"crypto/ecdsa"
	"errors"
	"fmt"
	"github.com/btcsuite/btcd/btcec"
	"github.com/ethereum/go-ethereum"
	"github.com/ethereum/go-ethereum/accounts/abi"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/ethereum/go-ethereum/ethclient"
	"github.com/ethersphere/go-sw3-abi/sw3abi"
	"math/big"
	"strings"
	"sync/atomic"
)

var erc20ABI = mustParseABI(sw3abi.ERC20ABIv0_3_1)

const (
	DefaultBoostPercent = 30
)

type Wallet struct {
	client *ethclient.Client
	key    Key
	trxNo  *atomic.Int64
}

func New(client *ethclient.Client, key Key) *Wallet {
	return &Wallet{
		client: client,
		key:    key,
		trxNo:  &atomic.Int64{},
	}
}

func (w *Wallet) CainID(ctx context.Context) (int64, error) {
	id, err := w.client.NetworkID(ctx)
	if err != nil {
		return 0, err
	}

	return id.Int64(), nil
}

func (w *Wallet) TransferNative(
	ctx context.Context,
	cid int64,
	toAddr common.Address,
	amount *big.Int,
) error {
	err := w.sendTransaction(ctx, cid, toAddr, amount, nil)
	if err != nil {
		return fmt.Errorf("failed to make native coin transfer, %w", err)
	}

	return nil
}

func (w *Wallet) TransferERC20(
	ctx context.Context,
	cid int64,
	toAddr common.Address,
	amount *big.Int,
	token Token,
) error {
	callData, err := erc20ABI.Pack("transfer", toAddr, amount)
	if err != nil {
		return fmt.Errorf("failed to pack abi, %w", err)
	}

	err = w.sendTransaction(ctx, cid, token.Contract, nil, callData)
	if err != nil {
		return fmt.Errorf("failed to make ERC20 token transfer, %w", err)
	}

	return nil
}

func (w *Wallet) sendTransaction(
	ctx context.Context,
	cid int64,
	toAddr common.Address,
	amount *big.Int,
	callData []byte,
) error {
	chainID, err := w.client.NetworkID(ctx)
	if err != nil {
		return fmt.Errorf("failed to get network id, %w", err)
	}

	if chainID.Int64() != cid {
		return errors.New("wallet chain id does not match chain id for transfer")
	}

	_, publicKey, err := w.keys()
	if err != nil {
		return fmt.Errorf("failed to get wallet keys, %w", err)
	}

	fromAddress := crypto.PubkeyToAddress(*publicKey)

	nonce, err := w.nonce(ctx, fromAddress)
	if err != nil {
		return fmt.Errorf("failed to make nonce, %w", err)
	}
	gas, gasFeeCap, gasTipCap, err := w.calculateGas(ctx, ethereum.CallMsg{
		From: fromAddress,
		To:   &toAddr,
		Data: callData,
	})
	if err != nil {
		return err
	}

	tx := types.NewTx(&types.DynamicFeeTx{
		Nonce:     nonce,
		ChainID:   chainID,
		To:        &toAddr,
		Value:     amount,
		Gas:       gas,
		GasFeeCap: gasFeeCap,
		GasTipCap: gasTipCap,
		Data:      callData,
	})
	signedTx, err := w.SignTx(tx, chainID)
	if err != nil {
		return fmt.Errorf("failed to sign transaction, %w", err)
	}

	err = w.client.SendTransaction(ctx, signedTx)
	if err != nil {
		return fmt.Errorf("failed to send transaction, %w", err)
	}

	return nil
}

func (w *Wallet) calculateGas(ctx context.Context, msg ethereum.CallMsg) (uint64, *big.Int, *big.Int, error) {
	gas, err := w.client.EstimateGas(ctx, msg)
	if err != nil {
		return 0, nil, nil, err
	}

	gas *= 1 + (DefaultBoostPercent / 100)

	gasFeeCap, gasTipCap, err := w.suggestedFeeAndTip(ctx, DefaultBoostPercent)
	if err != nil {
		return 0, nil, nil, fmt.Errorf("failed to get suggested gas price, %w", err)
	}
	return gas, gasFeeCap, gasTipCap, nil
}

func (w *Wallet) suggestedFeeAndTip(ctx context.Context, boostPercent int) (*big.Int, *big.Int, error) {
	var err error
	gasPrice, err := w.client.SuggestGasPrice(ctx)
	if err != nil {
		return nil, nil, err
	}

	gasTipCap, err := w.client.SuggestGasTipCap(ctx)
	if err != nil {
		return nil, nil, err
	}

	gasTipCap = new(big.Int).Div(new(big.Int).Mul(big.NewInt(int64(boostPercent)+100), gasTipCap), big.NewInt(100))
	gasPrice = new(big.Int).Div(new(big.Int).Mul(big.NewInt(int64(boostPercent)+100), gasPrice), big.NewInt(100))
	gasFeeCap := new(big.Int).Add(gasTipCap, gasPrice)

	return gasFeeCap, gasTipCap, nil

}

// SignTx signs an ethereum transaction.
func (w *Wallet) SignTx(transaction *types.Transaction, chainID *big.Int) (*types.Transaction, error) {
	txSigner := types.NewLondonSigner(chainID)
	hash := txSigner.Hash(transaction).Bytes()
	// isCompressedKey is false here so we get the expected v value (27 or 28)
	signature, err := w.sign(hash, false)
	if err != nil {
		return nil, err
	}

	// v value needs to be adjusted by 27 as transaction.WithSignature expects it to be 0 or 1
	signature[64] -= 27
	return transaction.WithSignature(txSigner, signature)
}

// sign the provided hash and convert it to the ethereum (r,s,v) format.
func (w *Wallet) sign(sighash []byte, isCompressedKey bool) ([]byte, error) {
	privateECDSA, err := w.key.PrivateECDSA()
	if err != nil {
		return nil, err
	}
	signature, err := btcec.SignCompact(btcec.S256(), (*btcec.PrivateKey)(privateECDSA), sighash, false)
	if err != nil {
		return nil, err
	}

	// Convert to Ethereum signature format with 'recovery id' v at the end.
	v := signature[0]
	copy(signature, signature[1:])
	signature[64] = v
	return signature, nil
}

func (w *Wallet) nonce(ctx context.Context, addr common.Address) (uint64, error) {
	nonce, err := w.client.PendingNonceAt(ctx, addr)
	if err != nil {
		return 0, fmt.Errorf("failed to get nonce, %w", err)
	}

	nonce += uint64(w.trxNo.Add(1) - 1)

	return nonce, nil
}

func (w *Wallet) keys() (*ecdsa.PrivateKey, *ecdsa.PublicKey, error) {
	privateKey, err := w.key.PrivateECDSA()
	if err != nil {
		return nil, nil, err
	}

	publicKeyECDSA, err := w.key.PublicECDSA()
	if err != nil {
		return nil, nil, err
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
