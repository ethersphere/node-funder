package funder

type NamespaceNodes struct {
	Name        string
	NodeWallets []WalletInfo
}

type WalletInfo struct {
	Name    string
	Address string
	ChainID int64
}

type NodeInfo struct {
	Name    string
	Address string
}
