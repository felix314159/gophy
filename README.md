# Gophy
Gophy is p2p software written from scratch to implement and test ideas for a novel blockchain network that is based on the idea of useful work. Its goal is to provide anyone the ability to donate computational power to a real-world High Energy Physics (HEP) experiment by running Monte Carlo simulation tasks sent out by a privileged node called the Root Authority (RA). The RA is a node that will be run by a representative of the experiment to ensure the 'usefulness' aspect of tasks.

## Table of Contents
* [Features](#features)

* [Build instructions](#building)

    * [Building gophy](#buildingGophy)
    * [Building CbmRoot](#buildingCbmroot)

* [Usage](#usage)

* [Contribution](#contribution)

* [Documentation](#documentation)

* [Work in progress](#wip)

* [Credits](#credits)

* [License](#license)

## Features
todo
<a name="features"/>

## Build instructions
todo
<a name="building"/>
### Building gophy
todo
<a name="buildingGophy"/>
### Building CbmRoot
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
