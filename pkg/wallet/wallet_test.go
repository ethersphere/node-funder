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
	// startNonce is deliberately not zero, so that a nonce which is reused and
	// one which is freshly fetched can be told apart.
	startNonce = 7
)

var errSendFailed = errors.New("mock error: cannot send transaction")

// Lookups are kept short, so that a test whose transaction never turns up on the
// chain does not sit out the real window.
const (
	shortLookupTimeout  = 100 * time.Millisecond
	shortLookupInterval = time.Millisecond
)

// Test_Send_RetriesWithSameNonce covers retrying a transfer whose send failed.
// Every attempt must reuse the nonce reserved by the first one, since two
// transactions sharing a nonce can never both be mined, which is what makes a
// retry safe regardless of how far the failed attempt got.
//
// It must also re-send the very same signed transaction. A re-signed attempt
// would carry a different hash, and then neither the lookup nor an "already
// known" answer could tell whether an earlier attempt had landed.
func Test_Send_RetriesWithSameNonce(t *testing.T) {
	t.Parallel()

	c := newScriptedClient()
	c.sendErrs = []error{errSendFailed, errSendFailed}

	w := newWallet(t, c, wallet.WithRetry(3, 0))

	require.NoError(t, w.TransferNative(context.Background(), common.HexToAddress(testAddress), big.NewInt(1)))
	assert.Equal(t, []uint64{startNonce, startNonce, startNonce}, c.sentNonces())

	hashes := c.sentHashes()
	require.Len(t, hashes, 3)
	assert.Equal(t, []common.Hash{hashes[0], hashes[0], hashes[0]}, hashes,
		"every attempt must re-send the transaction signed for the first one")
}

// Test_Send_AttemptsBelowOneSendOnce covers the floor on the attempt count. A
// zero or negative count must still send once and then stop, rather than leaving
// the retry loop without a way out.
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

// Test_Send_GasFailureDoesNotConsumeNonce covers a transfer that fails before it
// is ever broadcast. It must not consume the nonce, otherwise it leaves a gap
// behind which every later transaction queues forever.
func Test_Send_GasFailureDoesNotConsumeNonce(t *testing.T) {
	t.Parallel()

	c := newScriptedClient()
	c.gasFailures = -1 // fail until told otherwise

	w := newWallet(t, c, wallet.WithRetry(2, 0))
	addr := common.HexToAddress(testAddress)

	require.Error(t, w.TransferNative(context.Background(), addr, big.NewInt(1)))
	require.Empty(t, c.sentNonces(), "nothing should have been broadcast")

	c.gasFailures = 0

	require.NoError(t, w.TransferNative(context.Background(), addr, big.NewInt(1)))
	assert.Equal(t, []uint64{startNonce}, c.sentNonces(), "the unused nonce must still be available")
}

// Test_Send_AcceptedDespiteSendError covers a send whose response was lost
// although the node did take the transaction. Asking the chain about the hash
// settles it, so the transfer counts as done and its nonce stays used.
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
	assert.Equal(t, []uint64{startNonce, startNonce + 1}, c.sentNonces(),
		"the nonce of a transaction on the chain must not be handed out again")
}

// Test_Send_BroadcastNonceIsNeverReused covers a send which failed after the
// transaction was already handed to the node. A lost response cannot be told
// apart from a rejection over a single RPC hop, and the endpoint may front
// several nodes, so a lookup answering NotFound does not prove the transaction
// is absent. Reusing such a nonce puts two different transactions on it, and
// whichever loses leaves an address unfunded while its caller was told the
// transfer succeeded.
//
// The errors which mean the nonce is held by someone else's transaction are
// covered too: handing those back would keep the sender grinding on a nonce that
// can never be accepted, so nothing would ever get funded.
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

			assert.Equal(t, []uint64{startNonce, startNonce + 1, startNonce + 2}, c.sentNonces(),
				"each transfer must move on to a fresh nonce")
		})
	}
}

// Test_Send_ConcurrentFailuresNeverShareNonce covers the shape the funder
// actually runs: many goroutines sharing one sender. A nonce burned by a failed
// broadcast must not resurface for a later transfer, however the reservations
// and the failures interleave.
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

	assert.Len(t, seen, len(nonces), "a broadcast nonce must never be broadcast again: %v", nonces)
}

// Test_Send_LandedTransactionSurvivesCancellation covers the deadline firing
// while the send is in flight. The node kept the transaction, so the transfer
// must not be reported as failed, which means the lookup needs a context of its
// own rather than the one that just expired.
func Test_Send_LandedTransactionSurvivesCancellation(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	c := newScriptedClient()
	c.cancelOnSend = cancel
	c.sendErr = context.Canceled
	c.accepted = true

	w := newWallet(t, c)

	require.NoError(t, w.TransferNative(ctx, common.HexToAddress(testAddress), big.NewInt(1)),
		"a transaction the chain holds must not be reported as failed because the context died")
}

// Test_Send_AlreadyKnownCountsAsSent covers a node answering that it already
// holds the transaction. That is a positive statement about our own hash, so it
// settles the transfer even when a lookup cannot, which is what happens when the
// endpoint fronts several nodes and the lookup reaches a different one.
func Test_Send_AlreadyKnownCountsAsSent(t *testing.T) {
	t.Parallel()

	c := newScriptedClient()
	c.sendErrs = []error{errSendFailed, errors.New("already known")}
	c.accepted = false // the lookup reaches a node which has not seen it

	w := newWallet(t, c, wallet.WithRetry(2, 0))

	require.NoError(t, w.TransferNative(context.Background(), common.HexToAddress(testAddress), big.NewInt(1)))
	assert.Equal(t, []uint64{startNonce, startNonce}, c.sentNonces())
}

// Test_Send_EstimatesGasForTheValueSent covers the gas limit put on a transfer.
// The estimate must be asked for the very call being made, value included: a
// node estimates a transfer of nothing below the intrinsic cost of one that
// carries value, and the transaction then fails as "intrinsic gas too low". The
// estimate must also be boosted, so a transaction slightly costlier than
// estimated is not rejected.
func Test_Send_EstimatesGasForTheValueSent(t *testing.T) {
	t.Parallel()

	c := newScriptedClient()
	c.gasEstimate = 21_000

	w := newWallet(t, c)

	require.NoError(t, w.TransferNative(context.Background(), common.HexToAddress(testAddress), big.NewInt(42)))

	require.Len(t, c.gasCalls, 1)
	require.NotNil(t, c.gasCalls[0].Value, "the estimate must carry the value being sent")
	assert.Equal(t, int64(42), c.gasCalls[0].Value.Int64())

	gas := c.sentGas()
	require.Len(t, gas, 1)
	assert.Equal(t, uint64(27_300), gas[0], "the estimate must be boosted by 30%")
}

// Test_Send_LookupWaitsForTheTransactionToAppear covers a node rejecting a send
// it did in fact keep: Nethermind answers "replacement transaction underpriced"
// for a transaction already in its pool, and an endpoint fronting several nodes
// may answer before the transaction has reached the one asked. A single lookup
// right after the send misses such a transfer, so the chain is asked again for a
// while before the transfer is given up on.
func Test_Send_LookupWaitsForTheTransactionToAppear(t *testing.T) {
	t.Parallel()

	c := newScriptedClient()
	c.sendErr = errors.New("replacement transaction underpriced")
	c.knownAfter = 2 // the first two lookups answer NotFound

	w := newWallet(t, c)

	require.NoError(t, w.TransferNative(context.Background(), common.HexToAddress(testAddress), big.NewInt(1)))
	assert.Equal(t, []uint64{startNonce}, c.sentNonces(), "the transfer must not be re-sent")
	assert.Equal(t, 3, c.lookupCount(), "the lookup must be repeated until the chain holds the transaction")
}

// Test_Send_LookupGivesUpAfterTimeout covers a transaction which never turns up.
// The chain is asked more than once, but not forever: once the window is over,
// the transfer is reported as failed.
func Test_Send_LookupGivesUpAfterTimeout(t *testing.T) {
	t.Parallel()

	c := newScriptedClient()
	c.sendErr = errSendFailed

	w := newWallet(t, c)

	err := w.TransferNative(context.Background(), common.HexToAddress(testAddress), big.NewInt(1))
	require.ErrorIs(t, err, errSendFailed)
	assert.GreaterOrEqual(t, c.lookupCount(), 2, "the chain must be asked more than once")
}

// Test_Send_LookupWithoutIntervalAsksOnce covers an interval at or below zero,
// which must mean a single lookup rather than hammering the node for the whole
// window.
func Test_Send_LookupWithoutIntervalAsksOnce(t *testing.T) {
	t.Parallel()

	c := newScriptedClient()
	c.sendErr = errSendFailed

	w := newWallet(t, c, wallet.WithLookup(time.Second, 0))

	require.Error(t, w.TransferNative(context.Background(), common.HexToAddress(testAddress), big.NewInt(1)))
	assert.Equal(t, 1, c.lookupCount())
}

// newWallet builds a wallet on the scripted client with short lookups, which the
// given options may override.
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

// scriptedClient fails a configurable number of gas estimations and sends, and
// records the nonce of every transaction handed to the chain.
type scriptedClient struct {
	wallet.BackendClient

	mu            sync.Mutex
	gasFailures   int    // negative means always
	gasEstimate   uint64 // returned by every successful estimate; zero defers to the mock
	gasCalls      []ethereum.CallMsg
	gasPriceCalls int
	sendErrs      []error // consumed one per send, before sendErr applies
	sendErr       error   // returned by every further send
	accepted      bool    // whether the chain admits to holding the transactions
	knownAfter    int     // lookups answering NotFound before the chain admits to holding the transactions
	lookups       int
	cancelOnSend  func() // fires while the send is in flight
	nonces        []uint64
	hashes        []common.Hash
	gas           []uint64
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
	return startNonce, nil
}

// SuggestGasPrice moves with every call, the way a real one does, so that a
// transaction signed a second time cannot come out with the same hash by
// accident.
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

	if c.accepted || (c.knownAfter > 0 && c.lookups > c.knownAfter) {
		return types.NewTx(&types.DynamicFeeTx{}), true, nil
	}

	return nil, false, ethereum.NotFound
}
