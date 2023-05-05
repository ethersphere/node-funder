// Copyright 2022 The Swarm Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package mock

import (
	"context"

	"github.com/ethersphere/node-funder/pkg/funder"
)

func NewNodeLister(nodes []funder.NodeInfo) funder.NodeLister {
	return &nodeLister{nodes: nodes}
}

type nodeLister struct {
	nodes []funder.NodeInfo
}

func (nl *nodeLister) List(ctx context.Context, namespace string) ([]funder.NodeInfo, error) {
	return nl.nodes, nil
}
