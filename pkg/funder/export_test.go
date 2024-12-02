// Copyright 2023 The Swarm Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package funder

import "math/big"

func CalcTopUpAmount(minVal float64, currAmount *big.Int, decimals int) *big.Int {
	return calcTopUpAmount(minVal, currAmount, decimals)
}

func FormatAmount(amount *big.Int, decimals int) string {
	return formatAmount(amount, decimals)
}
