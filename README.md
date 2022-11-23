# node-funder

Node funder is tool to fund (top up) bee nodes in specifed k8s namespace up to the specifed amount.

## run node-funder

Arguments:
- `namespace` - the k8s namespace
- `chainNodeEndpoint` - RPC URL of blockchain node (Infura API URL)
- `walletKey` - private key of wallet which will be used to fund nodes (hex encoded string value).
- `minSwarm` - min amount of Swarm tokens node should have (on mainnet this is BZZ). Node is not funded if it already has more then specifed. 
- `minNative` - min amount of blockchain native tokens node should have (on mainnet this is ETH). Node is not funded if it already has more then specifed. 


```console
go run ./cmd -namespace={...} -chainNodeEndpoint={...} -walletKey={...} -minSwarm=10 -minNative=0.5

## example
## go run ./cmd -namespace="testnet" -chainNodeEndpoint="wss://goerli.infura.io/ws/v3/apikey" -walletKey="aaabbccddeeffdfd391e07b86b63ff7558ad711fed058461d0e4ceaae3cbebf16a" -minSwarm=10 -minNative=0.5
```
