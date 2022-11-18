// Copyright 2022 The Swarm Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package funder

import (
	"context"
	"fmt"

	"github.com/ethersphere/node-funder/pkg/kube"
)

func FundAllNodes(cfg Config) error {
	kubeClient, err := kube.NewKube()
	if err != nil {
		return fmt.Errorf("connecting kube client with error: %w", err)
	}

	_, err = kube.FetchNamespaceNodeInfo(context.Background(), kubeClient, cfg.Namespace)
	if err != nil {
		return fmt.Errorf("get node info failed with error: %w", err)
	}

	return nil
}
