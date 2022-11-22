// Copyright 2022 The Swarm Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package wallet

import (
	"crypto/ecdsa"
	"encoding/hex"
	"fmt"

	"github.com/ethereum/go-ethereum/crypto"
)

type Key string

func (k Key) Private() (*ecdsa.PrivateKey, error) {
	privateKey, err := crypto.HexToECDSA(string(k))
	if err != nil {
		return nil, err
	}

	return privateKey, nil
}

func (k Key) Public() (*ecdsa.PublicKey, error) {
	privateKey, err := k.Private()
	if err != nil {
		return nil, err
	}

	publicKeyECDSA, ok := privateKey.Public().(*ecdsa.PublicKey)
	if !ok {
		return nil, fmt.Errorf("failed to get public key from private key")
	}

	return publicKeyECDSA, nil
}

func GenerateKey() (Key, error) {
	privateKey, err := crypto.GenerateKey()
	if err != nil {
		return "", err
	}

	privateKeyBytes := crypto.FromECDSA(privateKey)
	keyStr := hex.EncodeToString(privateKeyBytes)

	return Key(keyStr), nil
}
