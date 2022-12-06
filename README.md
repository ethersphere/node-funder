# node-funder

[![lint](https://github.com/ethersphere/node-funder/actions/workflows/lint.yml/badge.svg?branch=main)](https://github.com/ethersphere/node-funder/actions/workflows/lint.yml)
[![test](https://github.com/ethersphere/node-funder/actions/workflows/test.yml/badge.svg?branch=main)](https://github.com/ethersphere/node-funder/actions/workflows/test.yml)

Node funder is tool to fund (top up) bee nodes up to the specifed amount. It can fund all nodes in k8s namespace or it can fund only specified addresses.

# run node-funder

## Arguments
- `chainNodeEndpoint` - RPC URL of blockchain node (Infura API URL)
- `walletKey` - private key of wallet which will be used to fund nodes (hex encoded string value).
- specify one argument: 
  - `namespace` - the k8s namespace to fund all nodes in this namespace, or
  - `addresses` - comma sparated list of wallet addresses (hex encoded string value) to fund wallets directly
- `minSwarm` - min amount of Swarm tokens node should have (on mainnet this is BZZ). Node is not funded if it already has more then specifed. 
- `minNative` - min amount of blockchain native tokens node should have (on mainnet this is ETH). Node is not funded if it already has more then specifed. 

## Command examples


### Fund nodes in k8s namespace

```console
## Fund nodes in k8s namespace to have at least 10 Swarm and 0.5 native tokens

go run ./cmd -chainNodeEndpoint={...} -walletKey={...} -namespace={...} -minSwarm=10 -minNative=0.5

## example
## go run ./cmd -chainNodeEndpoint="wss://goerli.infura.io/ws/v3/apikey" -walletKey="aaabbccddeeffdfd391e07b86b63ff7558ad711fed058461d0e4ceaae3cbebf16a" -namespace="testnet" -minSwarm=10 -minNative=0.5
```

### Fund addresses

```console
## Fund wallet addresses to have at least 10 Swarm and 0.5 native tokens

go run ./cmd -chainNodeEndpoint={...} -walletKey={...} -addresses={...} -minSwarm=10 -minNative=0.5

## example
## go run ./cmd -chainNodeEndpoint="wss://goerli.infura.io/ws/v3/apikey" -walletKey="aaabbccddeeffdfd391e07b86b63ff7558ad711fed058461d0e4ceaae3cbebf16a" -addresses="0x4C4E453E72aF9939A27cac5a09ba583d72c4DfF0,0x4C4E453E72aF9939A27cac5a09ba583d72c4DfF0" -minSwarm=10 -minNative=0.5
```
