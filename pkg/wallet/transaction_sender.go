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

	// alreadyKnownMsg is what a node answers when it already holds the
	// transaction being sent (go-ethereum's txpool.ErrAlreadyKnown). It arrives
	// as a JSON-RPC error carrying no sentinel to compare against, so the
	// message is matched instead.
	alreadyKnownMsg = "already known"
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

	return s.sendWithNonce(ctx, chainID, toAddr, fromAddress, amount, callData)
}

// sendWithNonce reserves a nonce and retries the send with that same nonce, so
// that at most one attempt can ever be mined.
//
// Gas is estimated before the nonce is reserved: it needs no nonce and is the
// likeliest thing to fail over a flaky endpoint, so a failure there must not hold
// one. Once the transaction has been handed to the node the nonce is spent for
// good - a lost response cannot be told apart from a rejection over a single RPC
// hop, and an endpoint fronting several nodes cannot prove the node did not keep
// it. Handing such a nonce back would put a later, different transaction on it,
// and whichever of the two loses leaves an address unfunded.
func (s *transactionSender) sendWithNonce(
	ctx context.Context,
	chainID *big.Int,
	toAddr common.Address,
	fromAddr common.Address,
	amount *big.Int,
	callData []byte,
) error {
	// The value goes into the estimate as well: a node estimates a transfer of
	// nothing below the intrinsic cost of one that carries value.
	gas, gasFeeCap, gasTipCap, err := s.calculateGas(ctx, ethereum.CallMsg{
		From:  fromAddr,
		To:    &toAddr,
		Value: amount,
		Data:  callData,
	})
	if err != nil {
		return err
	}

	nonce, err := s.reserveNonce(ctx, fromAddr)
	if err != nil {
		return fmt.Errorf("failed to make nonce, %w", err)
	}

	// Signed once per nonce, so that every attempt carries the same hash and a
	// lost response can be settled by asking the chain about it.
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
		return fmt.Errorf("failed to sign transaction, %w", err)
	}

	return s.sendAttempts(ctx, signedTx)
}

// sendAttempts sends the same signed transaction until the node is known to hold
// it or the attempts run out. Re-sending is safe because every attempt is byte
// identical, so the node either takes it once or says it already has it.
func (s *transactionSender) sendAttempts(ctx context.Context, signedTx *types.Transaction) error {
	attempts := max(s.opts.retryAttempts, 1)

	for attempt := 1; ; attempt++ {
		sendErr := s.client.SendTransaction(ctx, signedTx)
		if sendErr == nil || strings.Contains(sendErr.Error(), alreadyKnownMsg) {
			return nil
		}

		err := fmt.Errorf("failed to send transaction, %w", sendErr)

		// A send whose response was lost, or which the node rejected although it
		// kept the transaction, still leaves it with the node, so that a transfer
		// which did land is not reported as failed. This also settles a "nonce
		// too low" answer: the lookup finds the transaction if a previous attempt
		// was mined, and does not if the nonce was simply stale.
		if s.transactionKnown(ctx, signedTx.Hash()) {
			return nil
		}

		if attempt == attempts {
			return err
		}

		if waitErr := s.waitBeforeRetry(ctx); waitErr != nil {
			return errors.Join(err, waitErr)
		}
	}
}

// transactionKnown reports whether the node holds the transaction, pending or
// mined. The chain is asked repeatedly for the lookup window, because a
// transaction the node kept despite rejecting the send, or one still on its way
// to the node asked, is not there the instant the send returns. It runs on a
// context of its own, because the caller's may be exactly what failed the send
// and then the question could never be answered. Anything other than a definite
// yes counts as unknown, so a transfer is never reported as done without the
// chain saying so.
func (s *transactionSender) transactionKnown(ctx context.Context, hash common.Hash) bool {
	timeout, interval := s.opts.lookupTimeout, s.opts.lookupInterval
	once := timeout <= 0 || interval <= 0

	if timeout <= 0 {
		timeout = defaultLookupTimeout
	}

	ctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), timeout)
	defer cancel()

	for {
		tx, _, err := s.client.TransactionByHash(ctx, hash)
		if err == nil && tx != nil {
			return true
		}

		if once {
			return false
		}

		select {
		case <-time.After(s.opts.lookupInterval):
		case <-ctx.Done():
			return false
		}
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

func (s *transactionSender) calculateGas(ctx context.Context, msg ethereum.CallMsg) (uint64, *big.Int, *big.Int, error) {
	gas, err := s.client.EstimateGas(ctx, msg)
	if err != nil {
		return 0, nil, nil, err
	}

	gas = gas * (100 + defaultBoostPercent) / 100

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
