// Copyright 2022 The Swarm Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package wallet

import (
	"context"
	"crypto/ecdsa"
	"fmt"
	"math/big"
	"sync"

	"github.com/btcsuite/btcd/btcec"
	"github.com/ethereum/go-ethereum"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/crypto"
)

const (
	defaultBoostPercent = 30
	txSendMaxRetries    = 3
)

type TransactionSender interface {
	Send(
		ctx context.Context,
		toAddr common.Address,
		amount *big.Int,
		callData []byte,
	) error
}

type transactionSender struct {
	client    BackendClient
	key       Key
	nonceLock sync.Mutex
	nonceLast uint64
}

func newTransactionSender(client BackendClient, key Key) TransactionSender {
	return &transactionSender{
		client: client,
		key:    key,
	}
}

func (s *transactionSender) Send(
	ctx context.Context,
	toAddr common.Address,
	amount *big.Int,
	callData []byte,
) error {
	chainID, err := s.client.ChainID(ctx)
	if err != nil {
		return fmt.Errorf("failed to get network id, %w", err)
	}

	_, publicKey, err := s.keys()
	if err != nil {
		return fmt.Errorf("failed to get wallet keys, %w", err)
	}

	fromAddress := crypto.PubkeyToAddress(*publicKey)

	for i := 0; i < txSendMaxRetries; i++ {
		err = s.send(ctx, chainID, toAddr, fromAddress, amount, callData)
		if err != nil && err.Error() == "replacement transaction underpriced" {
			s.clearNonce()
			continue
		}

		return err
	}

	return err
}

func (s *transactionSender) send(
	ctx context.Context,
	chainID *big.Int,
	toAddr common.Address,
	fromAddr common.Address,
	amount *big.Int,
	callData []byte,
) error {
	nonce, err := s.nonce(ctx, fromAddr)
	if err != nil {
		return fmt.Errorf("failed to make nonce, %w", err)
	}

	gas, gasFeeCap, gasTipCap, err := s.calculateGas(ctx, ethereum.CallMsg{
		From: fromAddr,
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

	signedTx, err := s.signTx(tx, chainID)
	if err != nil {
		return fmt.Errorf("failed to sign transaction, %w", err)
	}

	err = s.client.SendTransaction(ctx, signedTx)
	if err != nil {
		return fmt.Errorf("failed to send transaction, %w", err)
	}

	return nil
}

func (s *transactionSender) calculateGas(ctx context.Context, msg ethereum.CallMsg) (uint64, *big.Int, *big.Int, error) {
	gas, err := s.client.EstimateGas(ctx, msg)
	if err != nil {
		return 0, nil, nil, err
	}

	gas *= 1 + (defaultBoostPercent / 100)

	gasFeeCap, gasTipCap, err := s.suggestedFeeAndTip(ctx, defaultBoostPercent)
	if err != nil {
		return 0, nil, nil, fmt.Errorf("failed to get suggested gas price, %w", err)
	}

	return gas, gasFeeCap, gasTipCap, nil
}

func (s *transactionSender) suggestedFeeAndTip(ctx context.Context, boostPercent int) (*big.Int, *big.Int, error) {
	var err error

	gasPrice, err := s.client.SuggestGasPrice(ctx)
	if err != nil {
		return nil, nil, err
	}

	gasTipCap, err := s.client.SuggestGasTipCap(ctx)
	if err != nil {
		return nil, nil, err
	}

	gasTipCap = new(big.Int).Div(new(big.Int).Mul(big.NewInt(int64(boostPercent)+100), gasTipCap), big.NewInt(100))
	gasPrice = new(big.Int).Div(new(big.Int).Mul(big.NewInt(int64(boostPercent)+100), gasPrice), big.NewInt(100))
	gasFeeCap := new(big.Int).Add(gasTipCap, gasPrice)

	return gasFeeCap, gasTipCap, nil
}

func (s *transactionSender) signTx(transaction *types.Transaction, chainID *big.Int) (*types.Transaction, error) {
	txSigner := types.NewLondonSigner(chainID)
	hash := txSigner.Hash(transaction).Bytes()

	// isCompressedKey is false here so we get the expected v value (27 or 28)
	signature, err := s.sign(hash, false)
	if err != nil {
		return nil, err
	}

	// v value needs to be adjusted by 27 as transaction.WithSignature expects it to be 0 or 1
	signature[64] -= 27

	return transaction.WithSignature(txSigner, signature)
}

// sign the provided hash and convert it to the ethereum (r,s,v) format.
func (s *transactionSender) sign(sighash []byte, isCompressedKey bool) ([]byte, error) {
	privateECDSA, err := s.key.PrivateECDSA()
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

func (s *transactionSender) nonce(ctx context.Context, addr common.Address) (uint64, error) {
	s.nonceLock.Lock()
	defer s.nonceLock.Unlock()

	if s.nonceLast == 0 {
		nonce, err := s.client.PendingNonceAt(ctx, addr)
		if err != nil {
			return 0, fmt.Errorf("failed to get nonce, %w", err)
		}

		s.nonceLast = nonce
	} else {
		s.nonceLast += 1
	}

	return s.nonceLast, nil
}

func (s *transactionSender) clearNonce() {
	s.nonceLock.Lock()
	defer s.nonceLock.Unlock()

	s.nonceLast = 0
}

func (s *transactionSender) keys() (*ecdsa.PrivateKey, *ecdsa.PublicKey, error) {
	privateKey, err := s.key.PrivateECDSA()
	if err != nil {
		return nil, nil, err
	}

	publicKeyECDSA, err := s.key.PublicECDSA()
	if err != nil {
		return nil, nil, err
	}

	return privateKey, publicKeyECDSA, nil
}
