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

	"github.com/ethersphere/node-funder/pkg/util"
	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	corev1client "k8s.io/client-go/kubernetes/typed/core/v1"
	_ "k8s.io/client-go/plugin/pkg/client/auth/oidc"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"
	"k8s.io/client-go/tools/clientcmd/api"
)

const (
	beeWalletEndpoint = "/wallet"
)

type NamespaceNodes struct {
	Name  string
	Nodes []Node
}
type Node struct {
	Name       string
	IP         string
	WalletInfo WalletInfo
}

type WalletInfo struct {
	Address string
	ChainID int64
}

type TokenResponse struct {
	Node  Node
	Error error
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

func FetchNamespaceNodeInfo(ctx context.Context, kube *corev1client.CoreV1Client, namespace string) (*NamespaceNodes, error) {
	// List all Pods in our current Namespace.
	pods, err := kube.Pods(namespace).List(ctx, metav1.ListOptions{})
	if err != nil {
		return nil, fmt.Errorf("listing pod failed: %w", err)
	}

	tokenResponseC := make(chan TokenResponse, len(pods.Items))

	for _, pod := range pods.Items {
		go func(pod v1.Pod) {
			wi, err := FetchWalletInfo(ctx, pod.Status.PodIP)
			tokenResponseC <- TokenResponse{
				Node: Node{
					Name:       pod.Name,
					IP:         pod.Status.PodIP,
					WalletInfo: wi,
				},
				Error: err,
			}
		}(pod)
	}

	nodes := make([]Node, 0)

	for i := 0; i < len(pods.Items); i++ {
		res := <-tokenResponseC
		if res.Error == nil {
			nodes = append(nodes, res.Node)
		}
	}

	return &NamespaceNodes{
		Name:  namespace,
		Nodes: nodes,
	}, nil
}

func FetchWalletInfo(ctx context.Context, nodeAddress string) (WalletInfo, error) {
	// get wallet balance
	response, err := util.SendHTTPRequest(ctx, http.MethodGet, nodeAPIAddress(nodeAddress, beeWalletEndpoint), nil)
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

func nodeAPIAddress(nodeAddress, endpoint string) string {
	return fmt.Sprintf("http://%s:1635%s", nodeAddress, endpoint)
}
