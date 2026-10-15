// Copyright 2026 The Swarm Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package wallet_test

import (
	"context"
	"errors"
	"fmt"
	"math/big"
	"sync"
	"testing"
	"time"

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
	// startNonce is not zero, so a reused nonce and a freshly fetched one differ.
	startNonce = 7
)

var errSendFailed = errors.New("mock error: cannot send transaction")

// Short lookups keep tests whose transaction never shows up fast.
const (
	shortLookupTimeout  = 100 * time.Millisecond
	shortLookupInterval = time.Millisecond
)

// Test_Send_RetriesWithSameNonce checks that every retry re-sends the same
// signed transaction, so at most one attempt can be mined.
func Test_Send_RetriesWithSameNonce(t *testing.T) {
	t.Parallel()

	c := newScriptedClient()
	c.sendErrs = []error{errSendFailed, errSendFailed}

	w := newWallet(t, c, wallet.WithRetry(3, 0))

	require.NoError(t, w.TransferNative(context.Background(), common.HexToAddress(testAddress), big.NewInt(1)))
	assert.Equal(t, []uint64{startNonce, startNonce, startNonce}, c.sentNonces())

	hashes := c.sentHashes()
	require.Len(t, hashes, 3)
	assert.Equal(t, []common.Hash{hashes[0], hashes[0], hashes[0]}, hashes)
}

// Test_Send_AttemptsBelowOneSendOnce checks that an attempt count below one
// still sends once.
func Test_Send_AttemptsBelowOneSendOnce(t *testing.T) {
	t.Parallel()

	for _, attempts := range []int{0, -1} {
		t.Run(fmt.Sprintf("attempts=%d", attempts), func(t *testing.T) {
			t.Parallel()

			c := newScriptedClient()
			c.sendErr = errSendFailed

			w := newWallet(t, c, wallet.WithRetry(attempts, 0))

			require.Error(t, w.TransferNative(context.Background(), common.HexToAddress(testAddress), big.NewInt(1)))
			assert.Equal(t, []uint64{startNonce}, c.sentNonces())
		})
	}
}

// Test_Send_GasFailureDoesNotConsumeNonce checks that a transfer failing before
// it is sent leaves no nonce gap.
func Test_Send_GasFailureDoesNotConsumeNonce(t *testing.T) {
	t.Parallel()

	c := newScriptedClient()
	c.gasFailures = -1

	w := newWallet(t, c, wallet.WithRetry(2, 0))
	addr := common.HexToAddress(testAddress)

	require.Error(t, w.TransferNative(context.Background(), addr, big.NewInt(1)))
	require.Empty(t, c.sentNonces())

	c.gasFailures = 0

	require.NoError(t, w.TransferNative(context.Background(), addr, big.NewInt(1)))
	assert.Equal(t, []uint64{startNonce}, c.sentNonces())
}

// Test_Send_AcceptedDespiteSendError checks that a failed send whose transaction
// is on the chain counts as done and keeps its nonce used.
func Test_Send_AcceptedDespiteSendError(t *testing.T) {
	t.Parallel()

	c := newScriptedClient()
	c.sendErr = errSendFailed
	c.accepted = true

	w := newWallet(t, c, wallet.WithRetry(2, 0))
	addr := common.HexToAddress(testAddress)

	require.NoError(t, w.TransferNative(context.Background(), addr, big.NewInt(1)))

	c.sendErr = nil
	c.accepted = false

	require.NoError(t, w.TransferNative(context.Background(), addr, big.NewInt(1)))
	assert.Equal(t, []uint64{startNonce, startNonce + 1}, c.sentNonces())
}

// Test_Send_BroadcastNonceIsNeverReused checks that a failed send never hands
// its nonce back, since the node may still hold the transaction.
func Test_Send_BroadcastNonceIsNeverReused(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name    string
		sendErr error
	}{
		{name: "connection dropped", sendErr: errSendFailed},
		{name: "nonce too low", sendErr: errors.New("nonce too low")},
		{name: "replacement underpriced", sendErr: errors.New("replacement transaction underpriced")},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			c := newScriptedClient()
			c.sendErr = tc.sendErr

			w := newWallet(t, c)
			addr := common.HexToAddress(testAddress)

			for range 3 {
				require.Error(t, w.TransferNative(context.Background(), addr, big.NewInt(1)))
			}

			assert.Equal(t, []uint64{startNonce, startNonce + 1, startNonce + 2}, c.sentNonces())
		})
	}
}

// Test_Send_NonceTooLowResyncsNonce checks that a nonce taken by another sender
// moves the next nonce up to the node's pending nonce.
func Test_Send_NonceTooLowResyncsNonce(t *testing.T) {
	t.Parallel()

	c := newScriptedClient()
	c.pendingNonces = []uint64{startNonce, startNonce + 5}
	c.sendErrs = []error{errors.New("nonce too low")}

	w := newWallet(t, c)
	addr := common.HexToAddress(testAddress)

	require.Error(t, w.TransferNative(context.Background(), addr, big.NewInt(1)))
	require.NoError(t, w.TransferNative(context.Background(), addr, big.NewInt(1)))
	assert.Equal(t, []uint64{startNonce, startNonce + 5}, c.sentNonces())
}

// Test_Send_NonceResyncNeverMovesBack checks that a stale pending nonce does not
// hand out a nonce again.
func Test_Send_NonceResyncNeverMovesBack(t *testing.T) {
	t.Parallel()

	c := newScriptedClient()
	c.pendingNonces = []uint64{startNonce, 0}
	c.sendErrs = []error{errors.New("nonce too low")}

	w := newWallet(t, c)
	addr := common.HexToAddress(testAddress)

	require.Error(t, w.TransferNative(context.Background(), addr, big.NewInt(1)))
	require.NoError(t, w.TransferNative(context.Background(), addr, big.NewInt(1)))
	assert.Equal(t, []uint64{startNonce, startNonce + 1}, c.sentNonces())
}

// Test_Send_RejectedIsNotRetried checks that a transaction the node rejected is
// neither re-sent nor polled for.
func Test_Send_RejectedIsNotRetried(t *testing.T) {
	t.Parallel()

	for _, msg := range []string{
		"insufficient funds for gas * price + value",
		"intrinsic gas too low",
		"exceeds block gas limit",
		"nonce too low",
	} {
		t.Run(msg, func(t *testing.T) {
			t.Parallel()

			c := newScriptedClient()
			c.sendErr = errors.New(msg)

			w := newWallet(t, c, wallet.WithRetry(3, 0), wallet.WithLookup(time.Minute, time.Millisecond))

			require.Error(t, w.TransferNative(context.Background(), common.HexToAddress(testAddress), big.NewInt(1)))
			assert.Len(t, c.sentNonces(), 1)
			assert.Equal(t, 1, c.lookupCount())
		})
	}
}

// Test_Send_NonceTooLowAfterRetryPolls checks that "nonce too low" on a retry
// polls the chain, since the earlier attempt may be the one that was mined.
func Test_Send_NonceTooLowAfterRetryPolls(t *testing.T) {
	t.Parallel()

	c := newScriptedClient()
	c.sendErrs = []error{errSendFailed, errors.New("nonce too low")}
	c.knownAfterSends = 2

	w := newWallet(t, c, wallet.WithRetry(2, 0))

	require.NoError(t, w.TransferNative(context.Background(), common.HexToAddress(testAddress), big.NewInt(1)))
	assert.Equal(t, []uint64{startNonce, startNonce}, c.sentNonces())
}

// Test_Send_ConcurrentFailuresNeverShareNonce checks that concurrent transfers
// sharing one sender never broadcast the same nonce twice.
func Test_Send_ConcurrentFailuresNeverShareNonce(t *testing.T) {
	t.Parallel()

	const concurrent = 4

	c := newScriptedClient()
	c.sendErr = errSendFailed

	w := newWallet(t, c)
	addr := common.HexToAddress(testAddress)

	var wg sync.WaitGroup

	for range concurrent {
		wg.Go(func() {
			assert.Error(t, w.TransferNative(context.Background(), addr, big.NewInt(1)))
		})
	}

	wg.Wait()

	c.sendErr = nil

	require.NoError(t, w.TransferNative(context.Background(), addr, big.NewInt(1)))

	nonces := c.sentNonces()
	require.Len(t, nonces, concurrent+1)

	seen := make(map[uint64]struct{}, len(nonces))
	for _, nonce := range nonces {
		seen[nonce] = struct{}{}
	}

	assert.Len(t, seen, len(nonces), "nonces: %v", nonces)
}

// Test_Send_LandedTransactionSurvivesCancellation checks that a send cut short
// by its context still counts as done when the chain holds the transaction.
func Test_Send_LandedTransactionSurvivesCancellation(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	c := newScriptedClient()
	c.cancelOnSend = cancel
	c.sendErr = context.Canceled
	c.accepted = true

	w := newWallet(t, c)

	require.NoError(t, w.TransferNative(ctx, common.HexToAddress(testAddress), big.NewInt(1)))
}

// Test_Send_CancellationStopsLookup checks that cancelling the context ends the
// lookup instead of waiting out its window.
func Test_Send_CancellationStopsLookup(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	c := newScriptedClient()
	c.cancelOnSend = cancel
	c.sendErr = errSendFailed

	w := newWallet(t, c, wallet.WithLookup(time.Minute, time.Millisecond))

	start := time.Now()
	err := w.TransferNative(ctx, common.HexToAddress(testAddress), big.NewInt(1))

	require.ErrorIs(t, err, errSendFailed)
	assert.Less(t, time.Since(start), 5*time.Second)
	assert.GreaterOrEqual(t, c.lookupCount(), 1, "the chain must be asked at least once")
}

// Test_Send_AlreadyKnownCountsAsSent checks that a node saying it already holds
// the transaction settles the transfer, even when the lookup cannot.
func Test_Send_AlreadyKnownCountsAsSent(t *testing.T) {
	t.Parallel()

	for _, msg := range []string{"already known", "AlreadyKnown"} {
		t.Run(msg, func(t *testing.T) {
			t.Parallel()

			c := newScriptedClient()
			c.sendErrs = []error{errSendFailed, errors.New(msg)}

			w := newWallet(t, c, wallet.WithRetry(2, 0))

			require.NoError(t, w.TransferNative(context.Background(), common.HexToAddress(testAddress), big.NewInt(1)))
			assert.Equal(t, []uint64{startNonce, startNonce}, c.sentNonces())
		})
	}
}

// Test_Send_EstimatesGasForTheValueSent checks that the estimate includes the
// value sent and that the gas limit is boosted by 30%.
func Test_Send_EstimatesGasForTheValueSent(t *testing.T) {
	t.Parallel()

	c := newScriptedClient()
	c.gasEstimate = 21_000

	w := newWallet(t, c)

	require.NoError(t, w.TransferNative(context.Background(), common.HexToAddress(testAddress), big.NewInt(42)))

	require.Len(t, c.gasCalls, 1)
	require.NotNil(t, c.gasCalls[0].Value)
	assert.Equal(t, int64(42), c.gasCalls[0].Value.Int64())

	gas := c.sentGas()
	require.Len(t, gas, 1)
	assert.Equal(t, uint64(27_300), gas[0])
}

// Test_Send_LookupWaitsForTheTransactionToAppear checks that the chain is asked
// again until a transaction the node kept shows up.
func Test_Send_LookupWaitsForTheTransactionToAppear(t *testing.T) {
	t.Parallel()

	c := newScriptedClient()
	c.sendErr = errors.New("replacement transaction underpriced")
	c.knownAfter = 2

	w := newWallet(t, c)

	require.NoError(t, w.TransferNative(context.Background(), common.HexToAddress(testAddress), big.NewInt(1)))
	assert.Equal(t, []uint64{startNonce}, c.sentNonces())
	assert.Equal(t, 3, c.lookupCount())
}

// Test_Send_LookupGivesUpAfterTimeout checks that the lookup stops once its
// window is over and the transfer is reported as failed.
func Test_Send_LookupGivesUpAfterTimeout(t *testing.T) {
	t.Parallel()

	c := newScriptedClient()
	c.sendErr = errSendFailed

	w := newWallet(t, c)

	err := w.TransferNative(context.Background(), common.HexToAddress(testAddress), big.NewInt(1))
	require.ErrorIs(t, err, errSendFailed)
	assert.GreaterOrEqual(t, c.lookupCount(), 2)
}

// Test_Send_LookupWithoutIntervalAsksOnce checks that an interval at or below
// zero asks the chain once.
func Test_Send_LookupWithoutIntervalAsksOnce(t *testing.T) {
	t.Parallel()

	c := newScriptedClient()
	c.sendErr = errSendFailed

	w := newWallet(t, c, wallet.WithLookup(time.Second, 0))

	require.Error(t, w.TransferNative(context.Background(), common.HexToAddress(testAddress), big.NewInt(1)))
	assert.Equal(t, 1, c.lookupCount())
}

// newWallet builds a wallet on the scripted client with short lookups, which
// opts may override.
func newWallet(t *testing.T, c *scriptedClient, opts ...wallet.Option) *wallet.Wallet {
	t.Helper()

	opts = append([]wallet.Option{wallet.WithLookup(shortLookupTimeout, shortLookupInterval)}, opts...)

	return wallet.New(c, generateKey(t), opts...)
}

func generateKey(t *testing.T) wallet.Key {
	t.Helper()

	key, err := wallet.GenerateKey()
	require.NoError(t, err)

	return key
}

// scriptedClient fails gas estimations and sends as configured, and records
// every transaction sent.
type scriptedClient struct {
	wallet.BackendClient

	mu            sync.Mutex
	pendingNonces []uint64 // one per call, the last one repeating; empty means startNonce
	gasFailures   int      // negative means always
	gasEstimate   uint64   // zero defers to the mock
	gasCalls      []ethereum.CallMsg
	gasPriceCalls int
	sendErrs      []error // one per send, before sendErr applies
	sendErr       error
	accepted      bool // whether lookups find the transaction
	knownAfter    int  // lookups answering NotFound before the transaction is found
	// knownAfterSends makes lookups find the transaction once this many sends
	// were made, from the second lookup after that on.
	knownAfterSends int
	lookupsAfter    int
	lookups         int
	cancelOnSend    func()
	nonces          []uint64
	hashes          []common.Hash
	gas             []uint64
}

func newScriptedClient() *scriptedClient {
	return &scriptedClient{BackendClient: walletmock.NewBackendClient()}
}

func (c *scriptedClient) sentNonces() []uint64 {
	c.mu.Lock()
	defer c.mu.Unlock()

	return append([]uint64(nil), c.nonces...)
}

func (c *scriptedClient) sentHashes() []common.Hash {
	c.mu.Lock()
	defer c.mu.Unlock()

	return append([]common.Hash(nil), c.hashes...)
}

func (c *scriptedClient) lookupCount() int {
	c.mu.Lock()
	defer c.mu.Unlock()

	return c.lookups
}

func (c *scriptedClient) sentGas() []uint64 {
	c.mu.Lock()
	defer c.mu.Unlock()

	return append([]uint64(nil), c.gas...)
}

func (c *scriptedClient) PendingNonceAt(context.Context, common.Address) (uint64, error) {
	c.mu.Lock()
	defer c.mu.Unlock()

	switch len(c.pendingNonces) {
	case 0:
		return startNonce, nil
	case 1:
		return c.pendingNonces[0], nil
	}

	nonce := c.pendingNonces[0]
	c.pendingNonces = c.pendingNonces[1:]

	return nonce, nil
}

// SuggestGasPrice changes on every call, so a re-signed transaction cannot get
// the same hash by accident.
func (c *scriptedClient) SuggestGasPrice(context.Context) (*big.Int, error) {
	c.mu.Lock()
	defer c.mu.Unlock()

	c.gasPriceCalls++

	return big.NewInt(int64(20_000 + c.gasPriceCalls)), nil
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

	c.gasCalls = append(c.gasCalls, call)

	if c.gasEstimate != 0 {
		return c.gasEstimate, nil
	}

	return c.BackendClient.EstimateGas(ctx, call)
}

func (c *scriptedClient) SendTransaction(_ context.Context, tx *types.Transaction) error {
	c.mu.Lock()
	defer c.mu.Unlock()

	c.nonces = append(c.nonces, tx.Nonce())
	c.hashes = append(c.hashes, tx.Hash())
	c.gas = append(c.gas, tx.Gas())

	if c.cancelOnSend != nil {
		c.cancelOnSend()
	}

	if len(c.sendErrs) > 0 {
		err := c.sendErrs[0]
		c.sendErrs = c.sendErrs[1:]

		return err
	}

	return c.sendErr
}

func (c *scriptedClient) TransactionByHash(ctx context.Context, _ common.Hash) (*types.Transaction, bool, error) {
	if err := ctx.Err(); err != nil {
		return nil, false, err
	}

	c.mu.Lock()
	defer c.mu.Unlock()

	c.lookups++

	if c.knownAfterSends > 0 && len(c.nonces) >= c.knownAfterSends {
		c.lookupsAfter++
		if c.lookupsAfter > 1 {
			return types.NewTx(&types.DynamicFeeTx{}), true, nil
		}
	}

	if c.accepted || (c.knownAfter > 0 && c.lookups > c.knownAfter) {
		return types.NewTx(&types.DynamicFeeTx{}), true, nil
	}

	return nil, false, ethereum.NotFound
}
