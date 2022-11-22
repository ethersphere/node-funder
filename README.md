# node-funder
tool to fund bee nodes 

## run node funder

Node funder will top up all been nodes in specifed k8s namespace up to the specifed amount.

Arguments:
- `namespace` - the k8s namespace
- `chainNodeEndpoint` - RPC url of blockchain node (infura api url)
- `walletKey` - private key of wallet which will be used to fund nodes (hex encoded string value).
- `minSwarm` - min amount of Swarm tokens node should have. Node is not funded if it already has more then specifed. 
- `minNative` - min amount of ETH tokens node should have. Node is not funded if it already has more then specifed. 


```console
go run ./cmd -namespace={...} -chainNodeEndpoint={...} -walletKey={...} -minSwarm=10 -minNative=0.5

## example
## go run ./cmd -namespace="testnet" -chainNodeEndpoint="wss://goerli.infura.io/ws/v3/apikey" -walletKey="aaabbccddeeffdfd391e07b86b63ff7558ad711fed058461d0e4ceaae3cbebf16a" -minSwarm=10 -minNative=0.5
```
