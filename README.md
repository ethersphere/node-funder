# node-funder

[![test](https://github.com/ethersphere/node-funder/actions/workflows/test.yml/badge.svg?branch=main)](https://github.com/ethersphere/node-funder/actions/workflows/test.yml)
[![lint](https://github.com/ethersphere/node-funder/actions/workflows/lint.yml/badge.svg?branch=main)](https://github.com/ethersphere/node-funder/actions/workflows/lint.yml)
[![coverage](https://raw.githubusercontent.com/ethersphere/node-funder/badges/.badges/main/coverage.svg)](./.github/testcoverage.yml)

Node funder is tool to fund (top up) bee nodes up to the specified amount. It can fund all nodes in k8s namespace or it can fund only specified addresses.

# run node-funder

## Arguments
### Funding node
- `chainNodeEndpoint` - RPC URL of blockchain node (Infura API URL)
- `walletKey` - private key of wallet which will be used to fund nodes (hex encoded string value).
- specify one argument: 
  - `namespace` - the k8s namespace to fund all nodes in this namespace, or
  - `addresses` - comma separated list of wallet addresses (hex encoded string value) to fund wallets directly
- `minSwarm` - min amount of Swarm tokens node should have (on mainnet this is xBZZ). Node is not funded if it already has more then specified. 
- `minNative` - min amount of blockchain native tokens node should have (on mainnet this is xDAI). Node is not funded if it already has more then specified. 

### Staking node
- `namespace` - the k8s namespace to stake all nodes in this namespace
- `minSwarm` - min amount of Swarm tokens node should have staked

## Command examples


### Fund nodes in k8s namespace

```console
## Fund nodes in k8s namespace to have at least 10 Swarm and 0.5 native tokens

go run ./cmd fund --chainNodeEndpoint={...} --walletKey={...} --namespace={...} --minSwarm=10 --minNative=0.5

## example
## go run ./cmd fund --chainNodeEndpoint="wss://goerli.infura.io/ws/v3/apikey" --walletKey="aaabbccddeeffdfd391e07b86b63ff7558ad711fed058461d0e4ceaae3cbebf16a" --namespace="testnet" --minSwarm=10 --minNative=0.5
```

### Fund addresses

```console
## Fund wallet addresses to have at least 10 Swarm and 0.5 native tokens

go run ./cmd fund --chainNodeEndpoint={...} --walletKey={...} --addresses={...} --minSwarm=10 --minNative=0.5

## example
## go run ./cmd fund --chainNodeEndpoint="wss://goerli.infura.io/ws/v3/apikey" --walletKey="aaabbccddeeffdfd391e07b86b63ff7558ad711fed058461d0e4ceaae3cbebf16a" --addresses="0x4C4E453E72aF9939A27cac5a09ba583d72c4DfF0,0x4C4E453E72aF9939A27cac5a09ba583d72c4DfF0" --minSwarm=10 --minNative=0.5
```

### Staking namespace

```console
## Stake all nodes to have at least 10 Swarm tokens staked

go run ./cmd stake --namespace={...} --minSwarm=10

## example
## go run ./cmd stake --namespace="testnet" --minSwarm=10
```
