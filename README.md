# Gophy
Gophy is p2p software written from scratch to implement and test ideas for a novel blockchain network that is based on the idea of useful work. Its goal is to provide anyone the ability to donate computational power to a real-world High Energy Physics (HEP) experiment by running Monte Carlo simulation tasks sent out by a privileged node called the Root Authority (RA). The RA is a node that will be run by a representative of the experiment to ensure the 'usefulness' aspect of tasks.

## Table of Contents
* [Features](#features)

* [Build instructions](#building)

    * [Building gophy](#buildingGophy)
    * [Building cbmroot](#buildingCbmroot)

* [Usage](#usage)

* [Contribution](#contribution)

* [Documentation](#documentation)

* [Work in progress](#wip)

* [Credits](#credits)

* [License](#license)

## Features
* Cross-platform compatibility
    * Tested under Windows 11, macOS Ventura and Ubuntu. Gophy should be able run on any platform that supports Golang. 
* Multiple kinds of nodes
    * Miner: A special kind of full node that actively runs simulation tasks to earn tradable tokens.
    * Full node: A node that helps other nodes to sync but does not act as a miner.
    * Light node: A node that helps other nodes to sync whenever possible but does not act as a miner. Lowers entry barriers to blockchain network due to lower computational and storage requirements.
* Transaction support
    * The blockchain features a nameless, tradable on-chain cryptocurrency that can be earned by performing useful work in the form of running simulation tasks. It is not compatible with other blockchains and has no inherent value.
* P2P networking
    * By making use of the libp2p network, gophy is able to let nodes from different network communicate even when they are behind a NAT/firewall. It should be noted that while holepunching in theory can work, go-libp2p does not guarantee that it works in every instance so it could be possible that it does not work for everyone.
* Novel blockchain architecture
    * In order to guarantee the usefulness of simulation tasks, a privileged node called the Root Authority (RA) define and send out new block problems in regular intervals. The RA is supposed to be controlled by a representative of the HEP experiment that profits from the simulation data the network generates. To mitigate some undeseriable side-affects of an increased degree of centralization, the blockchain architecture is designed in a way that nodes can control the RA which limits its power. However, it must be noted that in its current form the RA is both a necesssary evil (to preserve usefulness of tasks) and a single point of failure (no more block problems = no more blocks).
* HTTP API for sending transactions and new simulation tasks (default port 8087 can be overwritten via flag)
    * Gophy runs a local http server which provides a user-friendly way for nodes to broadcast transactions by visiting localhost:8087/send-transaction
    * It also provides a user-friendly way for the RA to broadcast a new block problem (simulation task) to the node network by visiting localhost:8087/send-simtask
* HTTP API for monitoring multiple locally ran instances of gophy
    * When running many nodes on the same machine via e.g. Docker it is easy to get an overview of what each node is doing by visiting localhost:12345
<a name="features"/>

## Build instructions
In order to build gophy you must install Golang. Follow official [Golang installation instructions](https://go.dev/doc/install) before continuing.
<a name="building"/>
### Building gophy
In order to statically build the gophy binary navigate to the root folder of this repo and then use one of the following commands depending on your OS:
* Windows:

```CGO_ENABLED=0 GOOS=windows GOARCH=amd64 go build .```
* MacOS:

```CGO_ENABLED=0 GOOS=darwin GOARCH=amd64 go build .```
* Linux-based OS:

```CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build .```

<a name="buildingGophy"/>
### Building cbmroot
This step is only necessary when you want to run a miner that actively works on simulation tasks and it can be skipped when you run gophy as a normal full or light node.

[CbmRoot](https://git.cbm.gsi.de/computing/cbmroot) is the analysis and simulation framework used by the CBM experiment at FAIR in Darmstadt. In order to use cbmroot you first need to acquire required external software that is contained in the [FairSoft](https://github.com/FairRootGroup/FairSoft) repository.
<a name="buildingCbmroot"/>

## Usage / Running gophy
todo
<a name="usage"/>
### Flags
todo
### Dashboard
todo
### Examples
todo

## Contribution
todo
<a name="contribution"/>

## Documentation
todo
<a name="documentation"/>

## WIP (Work in progress)
todo
<a name="wip"/>

## Credits
I would like to thank FIAS and HFHF for supporting my work. I would also like to thank my professor who provided valuable feedback over time.

Additionally, I want to give credits to the authors of the following Go packages, while I aimed to make use of Go's standard library or write my own code, in certain instances I relied on third-party code or on external packages that are developed outside of the Go core:
* [Go-libp2p](https://github.com/libp2p/go-libp2p) which is the networking stack used for peer discovery and data exchange between peers.
* [Bbolt](https://github.com/etcd-io/bbolt) which is the database used to store blockchain data.
* [Msgpack](https://github.com/vmihailenco/msgpack) which is a Go implementation of the MessagePack object serialization library.
* [Base58](https://github.com/btcsuite/btcd/tree/master/btcutil/base58) which is a Go implementation of the base58 plain text encoding scheme.
  
* [Crypto/SHA3](https://pkg.go.dev/golang.org/x/crypto/sha3) which is a Go implementation of SHA3 functions.
* [Crypto/SSH](https://pkg.go.dev/golang.org/x/crypto/ssh) which contains functions to serialize Ed25519 and other kinds of keys into the widely supported OpenSSH format.
<a name="credits"/>

## License
Gophy is licensed under the MIT license. For more information see [MIT license](https://opensource.org/license/MIT).
<a name="license"/>
