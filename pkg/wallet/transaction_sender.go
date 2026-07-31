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
	"sync"
	"time"

	"github.com/btcsuite/btcd/btcec/v2"
	btcececdsa "github.com/btcsuite/btcd/btcec/v2/ecdsa"
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
	opts      *options
	nonceLock sync.Mutex
	nonceNext uint64
	nonceInit bool
	// nonceFree holds nonces which were reserved but never broadcast. They are
	// handed out again before any new one, so that a failed transaction does
	// not leave a gap behind which every later one would queue forever.
	nonceFree []uint64
}

func newTransactionSender(client BackendClient, key Key, opts *options) TransactionSender {
	return &transactionSender{
		client: client,
		key:    key,
		opts:   opts,
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

	for range txSendMaxRetries {
		err = s.sendWithNonce(ctx, chainID, toAddr, fromAddress, amount, callData)
		if err != nil && err.Error() == "replacement transaction underpriced" {
			s.clearNonce()
			continue
		}

		return err
	}

	return err
}

// sendWithNonce reserves a nonce and retries the send with that same nonce, so
// that at most one attempt can ever be mined. The nonce goes back only once the
// chain has confirmed it holds no such transaction.
func (s *transactionSender) sendWithNonce(
	ctx context.Context,
	chainID *big.Int,
	toAddr common.Address,
	fromAddr common.Address,
	amount *big.Int,
	callData []byte,
) error {
	nonce, err := s.reserveNonce(ctx, fromAddr)
	if err != nil {
		return fmt.Errorf("failed to make nonce, %w", err)
	}

	nonceUsed, err := s.sendAttempts(ctx, nonce, chainID, toAddr, fromAddr, amount, callData)
	if !nonceUsed {
		s.releaseNonce(nonce)
	}

	return err
}

// sendAttempts sends the transaction, retrying it on failure, and reports
// whether its nonce may have been taken by the chain.
func (s *transactionSender) sendAttempts(
	ctx context.Context,
	nonce uint64,
	chainID *big.Int,
	toAddr common.Address,
	fromAddr common.Address,
	amount *big.Int,
	callData []byte,
) (nonceUsed bool, err error) {
	attempts := max(s.opts.retryAttempts, 1)

	var signedTx *types.Transaction

	for attempt := 1; attempt <= attempts; attempt++ {
		// Signed once per nonce, so that every attempt carries the same hash and
		// a lost response can be settled by asking the chain about it.
		if signedTx == nil {
			if signedTx, err = s.prepare(ctx, nonce, chainID, toAddr, fromAddr, amount, callData); err != nil {
				signedTx = nil
			}
		}

		if signedTx != nil {
			if err = s.client.SendTransaction(ctx, signedTx); err == nil {
				return true, nil
			}

			err = fmt.Errorf("failed to send transaction, %w", err)

			known, lookupErr := s.transactionKnown(ctx, signedTx.Hash())
			if known {
				return true, nil
			}

			// Only a lookup which answered rules the transaction out. One that
			// failed leaves the nonce possibly taken, so it must not go back.
			nonceUsed = nonceUsed || lookupErr != nil
		}

		if attempt == attempts {
			break
		}

		if waitErr := s.waitBeforeRetry(ctx); waitErr != nil {
			return nonceUsed, errors.Join(err, waitErr)
		}
	}

	return nonceUsed, err
}

// transactionKnown reports whether the node holds the transaction, pending or
// mined. An error means the question could not be answered.
func (s *transactionSender) transactionKnown(ctx context.Context, hash common.Hash) (bool, error) {
	tx, _, err := s.client.TransactionByHash(ctx, hash)

	switch {
	case errors.Is(err, ethereum.NotFound):
		return false, nil
	case err != nil:
		return false, err
	default:
		return tx != nil, nil
	}
}

func (s *transactionSender) waitBeforeRetry(ctx context.Context) error {
	select {
	case <-time.After(s.opts.retryDelay):
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

// prepare builds and signs the transaction for the given nonce.
func (s *transactionSender) prepare(
	ctx context.Context,
	nonce uint64,
	chainID *big.Int,
	toAddr common.Address,
	fromAddr common.Address,
	amount *big.Int,
	callData []byte,
) (*types.Transaction, error) {
	gas, gasFeeCap, gasTipCap, err := s.calculateGas(ctx, ethereum.CallMsg{
		From: fromAddr,
		To:   &toAddr,
		Data: callData,
	})
	if err != nil {
		return nil, err
	}

	signedTx, err := s.signTx(types.NewTx(&types.DynamicFeeTx{
		Nonce:     nonce,
		ChainID:   chainID,
		To:        &toAddr,
		Value:     amount,
		Gas:       gas,
		GasFeeCap: gasFeeCap,
		GasTipCap: gasTipCap,
		Data:      callData,
	}), chainID)
	if err != nil {
		return nil, fmt.Errorf("failed to sign transaction, %w", err)
	}

	return signedTx, nil
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
func (s *transactionSender) sign(sighash []byte, _ bool) ([]byte, error) {
	privateECDSA, err := s.key.PrivateECDSA()
	if err != nil {
		return nil, err
	}

	pvk, _ := btcec.PrivKeyFromBytes(privateECDSA.D.Bytes())

	signature, err := btcececdsa.SignCompact(pvk, sighash, false)
	if err != nil {
		return nil, err
	}

	// Convert to Ethereum signature format with 'recovery id' v at the end.
	v := signature[0]
	copy(signature, signature[1:])
	signature[64] = v

	return signature, nil
}

func (s *transactionSender) reserveNonce(ctx context.Context, addr common.Address) (uint64, error) {
	s.nonceLock.Lock()
	defer s.nonceLock.Unlock()

	if last := len(s.nonceFree) - 1; last >= 0 {
		nonce := s.nonceFree[last]
		s.nonceFree = s.nonceFree[:last]

		return nonce, nil
	}

	if !s.nonceInit {
		nonce, err := s.client.PendingNonceAt(ctx, addr)
		if err != nil {
			return 0, fmt.Errorf("failed to get nonce, %w", err)
		}

		s.nonceNext = nonce
		s.nonceInit = true
	}

	nonce := s.nonceNext
	s.nonceNext++

	return nonce, nil
}

func (s *transactionSender) releaseNonce(nonce uint64) {
	s.nonceLock.Lock()
	defer s.nonceLock.Unlock()

	s.nonceFree = append(s.nonceFree, nonce)
}

func (s *transactionSender) clearNonce() {
	s.nonceLock.Lock()
	defer s.nonceLock.Unlock()

	s.nonceInit = false
	s.nonceFree = nil
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
