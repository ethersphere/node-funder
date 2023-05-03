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

	"github.com/ethersphere/node-funder/pkg/util"
)

const (
	beeWalletEndpoint = "/wallet"
)

type NodeLister interface {
	List(ctx context.Context, namespace string) ([]NodeInfo, error)
}

func NewNodeLister() (NodeLister, error) {
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
		return nil, fmt.Errorf("failed listing pods: %v", err)
	}

	result := make([]NodeInfo, 0, len(pods.Items))
	for _, pod := range pods.Items {
		result = append(result, NodeInfo{
			Name:    pod.Name,
			Address: pod.Status.PodIP,
		})
	}

	return nil, nil
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

func FetchNamespaceNodeInfo(ctx context.Context, namespace string, nl NodeLister) (NamespaceNodes, error) {
	nodes, err := nl.List(ctx, namespace)
	if err != nil {
		return NamespaceNodes{}, fmt.Errorf("listing nodes failed: %w", err)
	}

	walletInfoResponseC := make(chan walletInfoResponse, len(nodes))

	for _, nodeInfo := range nodes {
		go func(nodeInfo NodeInfo) {
			wi, err := FetchWalletInfo(ctx, nodeInfo.Address)
			walletInfoResponseC <- walletInfoResponse{
				WalletInfo: WalletInfo{
					Name:    fmt.Sprintf("node (%s) (address=%s)", nodeInfo.Name, wi.Address),
					ChainID: wi.ChainID,
					Address: wi.Address,
				},
				Error: err,
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

func FetchWalletInfo(ctx context.Context, nodeAddress string) (WalletInfo, error) {
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
