// Copyright 2023 The Swarm Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package funder

import "fmt"

type NamespaceNodes struct {
	Name        string
	NodeWallets []WalletInfo
}

type WalletInfo struct {
	Name    string
	Address string
	ChainID int64
}

type NodeInfo struct {
	Name    string
	Address string
}

func NewWalletInfo(name, address string, chainID int64) WalletInfo {
	return WalletInfo{
		Name:    fmt.Sprintf("node (%s) (address=%s)", name, address),
		Address: address,
		ChainID: chainID,
	}
}
