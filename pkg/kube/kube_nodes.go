// Copyright 2022 The Swarm Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package kube

import (
	"context"
	"encoding/json"
	"fmt"
	"github.com/emicklei/go-restful/v3/log"
	"github.com/ethersphere/node-funder/pkg/util"
	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	corev1client "k8s.io/client-go/kubernetes/typed/core/v1"
	_ "k8s.io/client-go/plugin/pkg/client/auth/oidc"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"
	"k8s.io/client-go/tools/clientcmd/api"
	"math/big"
	"net/http"
	"os"
)

const (
	beeAddressEndpoint = "/addresses"
	beeWalletEndpoint  = "/wallet"
)

type NamespaceNodes struct {
	Name  string
	Nodes []Node
}
type Node struct {
	Name      string
	Ip        string
	BeeTokens BeeTokens
}

type BeeTokens struct {
	EthAddress string
	ChainID    int
	NativeCoin *big.Int
	BzzToken   *big.Int
}
type TokenResponse struct {
	Node  Node
	error error
}

func NewKube() (*corev1client.CoreV1Client, error) {
	config, err := GetConfig()
	if err != nil {
		return nil, fmt.Errorf("get configuration failed with error: %w", err)
	}

	// Create a Kubernetes core/v1 client.
	coreClient, err := corev1client.NewForConfig(config)
	if err != nil {
		return nil, fmt.Errorf("creating Kubernetes client failed with error: %w", err)
	}
	return coreClient, nil
}

func GetConfig() (*rest.Config, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return nil, fmt.Errorf("obtaining user's home dir: %w", err)
	}
	kubeconfigPath := home + "/.kube/config"
	return clientcmd.NewNonInteractiveDeferredLoadingClientConfig(
		&clientcmd.ClientConfigLoadingRules{ExplicitPath: kubeconfigPath},
		&clientcmd.ConfigOverrides{ClusterInfo: api.Cluster{Server: ""}}).ClientConfig()
}

func GetNodeInfo(ctx context.Context, kube *corev1client.CoreV1Client, namespace string) (*NamespaceNodes, error) {
	// List all Pods in our current Namespace.
	pods, err := kube.Pods(namespace).List(ctx, metav1.ListOptions{})
	if err != nil {
		return nil, fmt.Errorf("listing pod failed with error: %w", err)
	}

	tokenResponseC := make(chan TokenResponse, len(pods.Items))

	for _, pod := range pods.Items {
		go func(pod v1.Pod) {
			beeTokens, err := GetTokens(ctx, pod.Status.PodIP)
			tokenResponseC <- TokenResponse{Node: Node{
				Name:      pod.Name,
				Ip:        pod.Status.PodIP,
				BeeTokens: beeTokens}, error: err}
		}(pod)
	}
	nodes := make([]Node, 0)
	for i := 0; i < len(pods.Items); i++ {
		res := <-tokenResponseC
		if res.error == nil {
			nodes = append(nodes, res.Node)
		}
	}

	return &NamespaceNodes{
		Name:  namespace,
		Nodes: nodes,
	}, nil
}

func GetTokens(ctx context.Context, podAddress string) (BeeTokens, error) {

	// get eth address
	response, err := util.SendHTTPRequest(ctx, http.MethodGet, "application/json", fmt.Sprintf("http://%s:1635%s", podAddress, beeAddressEndpoint), nil)
	if err != nil {
		return BeeTokens{}, fmt.Errorf("get bee address failed with error: %w", err)
	}

	ethAddress := struct {
		EthereumAddress string `json:"ethereum"`
	}{}
	if err = json.Unmarshal(response, &ethAddress); err != nil {
		return BeeTokens{}, fmt.Errorf("authentication marshal error :%w", err)
	}

	response, err = util.SendHTTPRequest(ctx, http.MethodGet, "application/json", fmt.Sprintf("http://%s:1635%s", podAddress, beeWalletEndpoint), nil)
	if err != nil {
		return BeeTokens{}, fmt.Errorf("get bee address failed with error: %w", err)
	}

	tokens := struct {
		Bzz             string `json:"bzz"`
		XDai            string `json:"xDai"` // on mainnet this is NativeCoin and in testnet this is xDai
		ChainID         int    `json:"chainID"`
		ContractAddress string `json:"contractAddress"`
	}{}
	if err := json.Unmarshal(response, &tokens); err != nil {
		log.Printf("get bee wallet failed with address %s, error %v", podAddress, err)
		return BeeTokens{}, fmt.Errorf("authentication marshal error :%w", err)
	}

	return BeeTokens{
		EthAddress: ethAddress.EthereumAddress,
		NativeCoin: StringGweiToEth(tokens.XDai),
		BzzToken:   StringGweiToEth(tokens.Bzz),
		ChainID:    tokens.ChainID,
	}, nil
}

// StringGweiToEth converts gwei to eth
func StringGweiToEth(gwei string) *big.Int {
	eth := new(big.Int)
	eth.SetString(gwei, 10)
	return eth
}
