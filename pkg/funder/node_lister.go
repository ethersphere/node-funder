// Copyright 2023 The Swarm Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package funder

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	corev1client "k8s.io/client-go/kubernetes/typed/core/v1"
	_ "k8s.io/client-go/plugin/pkg/client/auth/oidc"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"
	"k8s.io/client-go/tools/clientcmd/api"
)

const (
	beeWalletEndpoint    = "/wallet"
	beeAddressesEndpoint = "/addresses"
)

type NodeLister interface {
	List(ctx context.Context, namespace string) ([]NodeInfo, error)
}

func newNodeLister() (NodeLister, error) {
	client, err := newKube()
	if err != nil {
		return nil, err
	}

	return &nodeLister{
		client: client,
	}, nil
}

type nodeLister struct {
	client *corev1client.CoreV1Client
}

func (nl *nodeLister) List(ctx context.Context, namespace string) ([]NodeInfo, error) {
	pods, err := nl.client.Pods(namespace).List(ctx, metav1.ListOptions{})
	if err != nil {
		return nil, fmt.Errorf("failed listing pods: %w", err)
	}

	result := make([]NodeInfo, 0, len(pods.Items))
	for _, pod := range pods.Items {
		result = append(result, NodeInfo{
			Name:    pod.Name,
			Address: fmt.Sprintf("http://%s:1635", pod.Status.PodIP),
		})
	}

	return result, nil
}

type walletInfoResponse struct {
	WalletInfo WalletInfo
	Error      error
}

func newKube() (*corev1client.CoreV1Client, error) {
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

func fetchNamespaceNodeInfo(ctx context.Context, namespace string, chainID int64, nl NodeLister) (NamespaceNodes, error) {
	nodes, err := nl.List(ctx, namespace)
	if err != nil {
		return NamespaceNodes{}, fmt.Errorf("listing nodes failed: %w", err)
	}

	walletInfoResponseC := make(chan walletInfoResponse, len(nodes))
	for _, nodeInfo := range nodes {
		go func(nodeInfo NodeInfo) {
			if chainID == 0 {
				wi, err := fetchWalletInfo(ctx, nodeInfo.Address)
				walletInfoResponseC <- walletInfoResponse{
					WalletInfo: NewWalletInfo(nodeInfo.Name, wi.Address, wi.ChainID),
					Error:      err,
				}
			} else {
				address, err := fetchAddressInfo(ctx, nodeInfo.Address)
				walletInfoResponseC <- walletInfoResponse{
					WalletInfo: NewWalletInfo(nodeInfo.Name, address, chainID),
					Error:      err,
				}
			}
		}(nodeInfo)
	}

	nodeWallets := make([]WalletInfo, 0)

	for i := 0; i < len(nodes); i++ {
		res := <-walletInfoResponseC
		if res.Error == nil {
			nodeWallets = append(nodeWallets, res.WalletInfo)
		}
	}

	return NamespaceNodes{
		Name:        namespace,
		NodeWallets: nodeWallets,
	}, nil
}

func fetchWalletInfo(ctx context.Context, nodeAddress string) (WalletInfo, error) {
	response, err := sendHTTPRequest(ctx, http.MethodGet, nodeAddress+beeWalletEndpoint)
	if err != nil {
		return WalletInfo{}, fmt.Errorf("get bee wallet info failed: %w", err)
	}

	walletResponse := struct {
		WalletAddress string `json:"walletAddress"`
		ChainID       int64  `json:"chainID"`
	}{}
	if err := json.Unmarshal(response, &walletResponse); err != nil {
		return WalletInfo{}, fmt.Errorf("failed to unmarshal wallet response :%w", err)
	}

	if walletResponse.WalletAddress == "" {
		return WalletInfo{}, fmt.Errorf("failed getting bee node wallet address")
	}

	return WalletInfo{
		Address: walletResponse.WalletAddress,
		ChainID: walletResponse.ChainID,
	}, nil
}

func fetchAddressInfo(ctx context.Context, nodeAddress string) (string, error) {
	response, err := sendHTTPRequest(ctx, http.MethodGet, nodeAddress+beeAddressesEndpoint)
	if err != nil {
		return "", fmt.Errorf("get bee wallet info failed: %w", err)
	}

	walletResponse := struct {
		WalletAddress string `json:"ethereum"`
	}{}
	if err := json.Unmarshal(response, &walletResponse); err != nil {
		return "", fmt.Errorf("failed to unmarshal wallet response :%w", err)
	}

	if walletResponse.WalletAddress == "" {
		return "", fmt.Errorf("failed getting bee node wallet address")
	}

	return walletResponse.WalletAddress, nil
}
