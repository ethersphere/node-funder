// Copyright 2023 The Swarm Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package mock

import (
	"context"
	"errors"
	"math/big"

	"github.com/ethereum/go-ethereum"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethersphere/node-funder/pkg/wallet"
)

func NewBackendClient() wallet.BackendClient {
	return client{}
}

type client struct{}

func (c client) NetworkID(ctx context.Context) (*big.Int, error) {
	return big.NewInt(100), nil
}

func (c client) CallContract(context.Context, ethereum.CallMsg, *big.Int) ([]byte, error) {
	return nil, errors.New("disabled chain backend")
}

func (c client) PendingNonceAt(context.Context, common.Address) (uint64, error) {
	return 0, nil
}

func (c client) SuggestGasPrice(context.Context) (*big.Int, error) {
	return big.NewInt(20_000), nil
}

func (c client) SuggestGasTipCap(context.Context) (*big.Int, error) {
	return big.NewInt(10), nil
}

func (c client) EstimateGas(context.Context, ethereum.CallMsg) (uint64, error) {
	return 10, nil
}

func (c client) SendTransaction(context.Context, *types.Transaction) error {
	return nil
}

func (c client) BalanceAt(context.Context, common.Address, *big.Int) (*big.Int, error) {
	return big.NewInt(100000000), nil
}
