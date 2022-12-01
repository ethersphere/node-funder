// Copyright 2022 The Swarm Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package kube

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"

	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	corev1client "k8s.io/client-go/kubernetes/typed/core/v1"
	_ "k8s.io/client-go/plugin/pkg/client/auth/oidc"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"
	"k8s.io/client-go/tools/clientcmd/api"

	"github.com/ethersphere/node-funder/pkg/types"
	"github.com/ethersphere/node-funder/pkg/util"
)

const (
	beeWalletEndpoint = "/wallet"
)

type walletInfoResponse struct {
	WalletInfo types.WalletInfo
	Error      error
}

func NewKube() (*corev1client.CoreV1Client, error) {
	config, err := makeConfig()
	if err != nil {
		return nil, fmt.Errorf("get configuration failed: %w", err)
	}

	coreClient, err := corev1client.NewForConfig(config)
	if err != nil {
		return nil, fmt.Errorf("creating Kubernetes client failed: %w", err)
	}

	return coreClient, nil
}

func makeConfig() (*rest.Config, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return nil, fmt.Errorf("obtaining user's home dir failed: %w", err)
	}

	kubeconfigPath := home + "/.kube/config"

	return clientcmd.NewNonInteractiveDeferredLoadingClientConfig(
		&clientcmd.ClientConfigLoadingRules{ExplicitPath: kubeconfigPath},
		&clientcmd.ConfigOverrides{ClusterInfo: api.Cluster{Server: ""}}).ClientConfig()
}

func FetchNamespaceNodeInfo(ctx context.Context, kube *corev1client.CoreV1Client, namespace string) (*types.NamespaceNodes, error) {
	// List all Pods in our current Namespace.
	pods, err := kube.Pods(namespace).List(ctx, metav1.ListOptions{})
	if err != nil {
		return nil, fmt.Errorf("listing pod failed: %w", err)
	}

	walletInfoResponseC := make(chan walletInfoResponse, len(pods.Items))

	for _, pod := range pods.Items {
		go func(pod v1.Pod) {
			wi, err := FetchWalletInfo(ctx, pod.Status.PodIP)
			walletInfoResponseC <- walletInfoResponse{
				WalletInfo: types.WalletInfo{
					Name:    fmt.Sprintf("node (%s) (address=%s)", pod.Name, wi.Address),
					ChainID: wi.ChainID,
					Address: wi.Address,
				},
				Error: err,
			}
		}(pod)
	}

	nodeWallets := make([]types.WalletInfo, 0)

	for i := 0; i < len(pods.Items); i++ {
		res := <-walletInfoResponseC
		if res.Error == nil {
			nodeWallets = append(nodeWallets, res.WalletInfo)
		}
	}

	return &types.NamespaceNodes{
		Name:        namespace,
		NodeWallets: nodeWallets,
	}, nil
}

func FetchWalletInfo(ctx context.Context, nodeAddress string) (types.WalletInfo, error) {
	response, err := util.SendHTTPRequest(ctx, http.MethodGet, nodeAPIAddress(nodeAddress, beeWalletEndpoint), nil)
	if err != nil {
		return types.WalletInfo{}, fmt.Errorf("get bee wallet info failed: %w", err)
	}

	walletResponse := struct {
		WalletAddress string `json:"walletAddress"`
		ChainID       int64  `json:"chainID"`
	}{}
	if err := json.Unmarshal(response, &walletResponse); err != nil {
		return types.WalletInfo{}, fmt.Errorf("failed to unmarshal wallet response :%w", err)
	}

	if walletResponse.WalletAddress == "" {
		return types.WalletInfo{}, fmt.Errorf("failed getting bee node wallet address")
	}

	return types.WalletInfo{
		Address: walletResponse.WalletAddress,
		ChainID: walletResponse.ChainID,
	}, nil
}

func nodeAPIAddress(nodeAddress, endpoint string) string {
	return fmt.Sprintf("http://%s:1635%s", nodeAddress, endpoint)
}
