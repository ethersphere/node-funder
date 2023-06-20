// Copyright 2023 The Swarm Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package funder_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	. "github.com/ethersphere/node-funder/pkg/funder"
	fundermock "github.com/ethersphere/node-funder/pkg/funder/mock"
	"github.com/stretchr/testify/assert"
)

func Test_Stake(t *testing.T) {
	t.Parallel()

	ctx := context.Background()

	t.Run("stake - empty", func(t *testing.T) {
		t.Parallel()

		cfg := Config{}
		nl := fundermock.NewNodeLister(nil)
		err := Stake(ctx, cfg, nl)
		assert.NoError(t, err)
	})

	t.Run("stake namespace - not a bee node", func(t *testing.T) {
		t.Parallel()

		nl := fundermock.NewNodeLister([]NodeInfo{{Name: "not-a-valid-beenode"}})
		cfg := Config{Namespace: "swarm"}
		err := Stake(ctx, cfg, nl)
		assert.NoError(t, err)
	})

	t.Run("stake namespace - valid bee node", func(t *testing.T) {
		t.Parallel()

		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
			_, err := w.Write([]byte(`
			{
				"stakedAmount": "5",
			}
			`))
			assert.NoError(t, err)
		}))
		t.Cleanup(server.Close)
		nl := fundermock.NewNodeLister([]NodeInfo{{Address: server.URL, Name: "bee"}})

		t.Run("already staked", func(t *testing.T) {
			t.Parallel()

			cfg := Config{Namespace: "swarm"}
			err := Stake(ctx, cfg, nl)
			assert.NoError(t, err)
		})

		t.Run("not staked", func(t *testing.T) {
			t.Parallel()

			cfg := Config{Namespace: "swarm", MinAmounts: MinAmounts{SwarmToken: 20}}
			err := Stake(ctx, cfg, nl)
			assert.NoError(t, err)
		})
	})
}
