// Copyright 2026 The Swarm Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package wallet_test

import (
	"context"
	"errors"
	"math/big"
	"sync"
	"testing"

	"github.com/ethereum/go-ethereum"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ethersphere/node-funder/pkg/wallet"
	walletmock "github.com/ethersphere/node-funder/pkg/wallet/mock"
)

const (
	testAddress = "0x95f8916183f7C7154e49396507F5b0FafA4d8077"
	// startNonce is deliberately not zero, so that a nonce which is reused and
	// one which is freshly fetched can be told apart.
	startNonce = 7
)

// Test_Send_RetriesWithSameNonce covers retrying a transfer whose send failed.
// Every attempt must reuse the nonce reserved by the first one, since two
// transactions sharing a nonce can never both be mined, which is what makes a
// retry safe regardless of how far the failed attempt got.
func Test_Send_RetriesWithSameNonce(t *testing.T) {
	t.Parallel()

	c := newScriptedClient()
	c.sendFailures = 2

	w := wallet.New(c, generateKey(t), wallet.WithRetry(3, 0))

	require.NoError(t, w.TransferNative(context.Background(), common.HexToAddress(testAddress), big.NewInt(1)))
	assert.Equal(t, []uint64{startNonce, startNonce, startNonce}, c.sentNonces())
}

// Test_Send_FailureFreesNonce covers a transfer that fails before it is ever
// broadcast. The reserved nonce must go back, otherwise it leaves a gap and
// every later transaction queues behind it forever.
func Test_Send_FailureFreesNonce(t *testing.T) {
	t.Parallel()

	c := newScriptedClient()
	c.gasFailures = -1 // fail until told otherwise

	w := wallet.New(c, generateKey(t), wallet.WithRetry(2, 0))
	addr := common.HexToAddress(testAddress)

	require.Error(t, w.TransferNative(context.Background(), addr, big.NewInt(1)))
	require.Empty(t, c.sentNonces(), "nothing should have been broadcast")

	c.gasFailures = 0

	require.NoError(t, w.TransferNative(context.Background(), addr, big.NewInt(1)))
	assert.Equal(t, []uint64{startNonce}, c.sentNonces(), "the freed nonce must be reused")
}

// Test_Send_AcceptedDespiteSendError covers a send whose response was lost
// although the node did take the transaction. Asking the chain about the hash
// settles it, so the transfer counts as done and its nonce stays used.
func Test_Send_AcceptedDespiteSendError(t *testing.T) {
	t.Parallel()

	c := newScriptedClient()
	c.sendFailures = -1
	c.accepted = true

	w := wallet.New(c, generateKey(t), wallet.WithRetry(2, 0))
	addr := common.HexToAddress(testAddress)

	require.NoError(t, w.TransferNative(context.Background(), addr, big.NewInt(1)))

	c.sendFailures = 0
	c.accepted = false

	require.NoError(t, w.TransferNative(context.Background(), addr, big.NewInt(1)))
	assert.Equal(t, []uint64{startNonce, startNonce + 1}, c.sentNonces(),
		"the nonce of a transaction on the chain must not be handed out again")
}

func generateKey(t *testing.T) wallet.Key {
	t.Helper()

	key, err := wallet.GenerateKey()
	require.NoError(t, err)

	return key
}

// scriptedClient fails a configurable number of gas estimations and sends, and
// records the nonce of every transaction that reaches the chain.
type scriptedClient struct {
	wallet.BackendClient

	mu           sync.Mutex
	gasFailures  int  // negative means always
	sendFailures int  // negative means always
	accepted     bool // whether the chain knows the transactions
	nonces       []uint64
}

func (c *scriptedClient) TransactionByHash(context.Context, common.Hash) (*types.Transaction, bool, error) {
	c.mu.Lock()
	defer c.mu.Unlock()

	if !c.accepted {
		return nil, false, ethereum.NotFound
	}

	return types.NewTx(&types.DynamicFeeTx{}), true, nil
}

func newScriptedClient() *scriptedClient {
	return &scriptedClient{BackendClient: walletmock.NewBackendClient()}
}

func (c *scriptedClient) sentNonces() []uint64 {
	c.mu.Lock()
	defer c.mu.Unlock()

	return append([]uint64(nil), c.nonces...)
}

func (c *scriptedClient) PendingNonceAt(context.Context, common.Address) (uint64, error) {
	return startNonce, nil
}

func (c *scriptedClient) EstimateGas(ctx context.Context, call ethereum.CallMsg) (uint64, error) {
	c.mu.Lock()
	defer c.mu.Unlock()

	if c.gasFailures != 0 {
		if c.gasFailures > 0 {
			c.gasFailures--
		}

		return 0, errors.New("mock error: cannot estimate gas")
	}

	return c.BackendClient.EstimateGas(ctx, call)
}

func (c *scriptedClient) SendTransaction(_ context.Context, tx *types.Transaction) error {
	c.mu.Lock()
	defer c.mu.Unlock()

	c.nonces = append(c.nonces, tx.Nonce())

	if c.sendFailures != 0 {
		if c.sendFailures > 0 {
			c.sendFailures--
		}

		return errors.New("mock error: cannot send transaction")
	}

	return nil
}
