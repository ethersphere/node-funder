// Copyright 2022 The Swarm Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package wallet

import (
	"encoding/hex"
	"fmt"
	"os"

	"github.com/ethereum/go-ethereum/crypto"
)

const (
	keyDir  = "./.data"
	keyPath = keyDir + "/wallet.key"
)

type WalletKey string

func GetKey() (WalletKey, error) {
	key, err := loadKey()
	if err == nil {
		return key, nil
	}

	key, err = generateKey()
	if err != nil {
		return "", fmt.Errorf("failed to generate wallet key: %w", err)
	}

	err = storeKey(key)
	if err != nil {
		return "", fmt.Errorf("failed to store wallet key: %w", err)
	}

	return key, nil
}

func storeKey(key WalletKey) error {
	const perm = 0o777

	if err := os.MkdirAll(keyDir, perm); err != nil {
		return err
	}

	return os.WriteFile(keyPath, []byte(key), perm)
}

func loadKey() (WalletKey, error) {
	data, err := os.ReadFile(keyPath)
	if err != nil {
		return "", err
	}

	return WalletKey(data), nil
}

func generateKey() (WalletKey, error) {
	privateKey, err := crypto.GenerateKey()
	if err != nil {
		return "", err
	}

	privateKeyBytes := crypto.FromECDSA(privateKey)
	keyStr := hex.EncodeToString(privateKeyBytes)

	return WalletKey(keyStr), nil
}
