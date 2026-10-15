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
	"slices"
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

const defaultBoostPercent = 30

// Nodes report these as plain JSON-RPC errors, so they are matched by message.
var (
	// The node already holds the transaction (geth, Nethermind).
	alreadyKnownMsgs = []string{"already known", "alreadyknown"}
	// The nonce is already used on chain.
	nonceTooLowMsgs = []string{"nonce too low"}
	// The node rejected the transaction itself, so re-sending it cannot help.
	rejectedMsgs = []string{"nonce too low", "insufficient funds", "intrinsic gas too low", "exceeds block gas limit"}
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
	nonceFree []uint64 // released nonces no node holds, sorted
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

// sendWithNonce signs the transaction once on a newly reserved nonce and sends
// it. The nonce is handed back only when the first send is rejected outright:
// any other failure cannot prove the node did not keep the transaction, and
// putting a different one on the same nonce could leave an address unfunded.
func (s *transactionSender) sendWithNonce(
	ctx context.Context,
	chainID *big.Int,
	toAddr common.Address,
	fromAddr common.Address,
	amount *big.Int,
	callData []byte,
) error {
	// Estimated before the nonce is reserved, so a failure does not burn one. The
	// value must be included, or the estimate can fall below the intrinsic gas.
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

	return s.sendAttempts(ctx, fromAddr, signedTx)
}

// sendAttempts sends the same signed transaction until the node holds it, the
// node rejects it, or the attempts run out.
func (s *transactionSender) sendAttempts(ctx context.Context, fromAddr common.Address, signedTx *types.Transaction) error {
	attempts := max(s.opts.retries, 0) + 1

	for attempt := 1; ; attempt++ {
		sendErr := s.client.SendTransaction(ctx, signedTx)
		if sendErr == nil || errorContains(sendErr, alreadyKnownMsgs) {
			return nil
		}

		err := fmt.Errorf("failed to send transaction, %w", sendErr)

		// The node may have kept the transaction despite the error, or an earlier
		// attempt may have been mined. A rejection is final, so the chain is asked
		// once, unless the nonce is gone after an earlier attempt that may be it.
		rejected := errorContains(sendErr, rejectedMsgs)
		nonceTooLow := errorContains(sendErr, nonceTooLowMsgs)
		poll := !rejected || (nonceTooLow && attempt > 1)
		if s.transactionKnown(ctx, signedTx.Hash(), poll) {
			return nil
		}

		switch {
		case nonceTooLow:
			s.resyncNonce(ctx, fromAddr)
		case rejected && attempt == 1:
			// Only a rejection of the first send proves that no node holds the
			// transaction, so its nonce can go to the next transfer.
			s.releaseNonce(signedTx.Nonce())
		}

		if rejected || attempt == attempts {
			return err
		}

		if waitErr := s.waitBeforeRetry(ctx); waitErr != nil {
			return errors.Join(err, waitErr)
		}
	}
}

// transactionKnown reports whether the chain holds the transaction, pending or
// mined. With poll set, it keeps asking for the lookup window. The first query
// runs even if ctx is done, since a send cut short by ctx may still have landed;
// after that, cancelling ctx stops the lookup.
func (s *transactionSender) transactionKnown(ctx context.Context, hash common.Hash, poll bool) bool {
	timeout, interval := s.opts.lookupTimeout, s.opts.lookupInterval
	if timeout <= 0 {
		timeout = defaultLookupTimeout
	}

	lookupCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), timeout)
	defer cancel()

	known := func() bool {
		tx, _, err := s.client.TransactionByHash(lookupCtx, hash)
		return err == nil && tx != nil
	}

	if known() {
		return true
	}

	if !poll || interval <= 0 {
		return false
	}

	stop := context.AfterFunc(ctx, cancel)
	defer stop()

	for {
		select {
		case <-time.After(interval):
		case <-lookupCtx.Done():
			return false
		}

		if known() {
			return true
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

	if len(s.nonceFree) > 0 {
		nonce := s.nonceFree[0]
		s.nonceFree = s.nonceFree[1:]

		return nonce, nil
	}

	nonce := s.nonceNext
	s.nonceNext++

	return nonce, nil
}

// releaseNonce hands back a nonce that no node holds. The next reservation takes
// it first, which fills the gap that transfers on later nonces wait behind.
func (s *transactionSender) releaseNonce(nonce uint64) {
	s.nonceLock.Lock()
	defer s.nonceLock.Unlock()

	if i, found := slices.BinarySearch(s.nonceFree, nonce); !found {
		s.nonceFree = slices.Insert(s.nonceFree, i, nonce)
	}
}

// resyncNonce moves the next nonce up to the node's pending nonce. It never
// moves it back, so a reserved nonce is not handed out twice.
func (s *transactionSender) resyncNonce(ctx context.Context, addr common.Address) {
	s.nonceLock.Lock()
	defer s.nonceLock.Unlock()

	nonce, err := s.client.PendingNonceAt(ctx, addr)
	if err != nil {
		return
	}

	s.nonceNext = max(s.nonceNext, nonce)
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

func errorContains(err error, msgs []string) bool {
	text := strings.ToLower(err.Error())

	return slices.ContainsFunc(msgs, func(msg string) bool {
		return strings.Contains(text, msg)
	})
}
