# Gophy
Gophy is p2p software written from scratch in Go to implement and test ideas for a novel blockchain network that is based on the idea of useful work. Its goal is to provide anyone the ability to donate computational power to a real-world High Energy Physics (HEP) experiment by running Monte Carlo simulation tasks sent out by a privileged node called the Root Authority (RA). The RA is a node that will be run by a representative of the experiment to ensure the 'usefulness' aspect of tasks.

## Table of Contents
* [Features](#features)

* [Build instructions](#building)

    * [Building gophy](#buildingGophy)
    * [Building cbmroot](#buildingCbmroot)

* [Docker setup](#docker)

* [Usage](#usage)

    * [Flags](#flags)
    * [Dashboard](#dashboard)
    * [Usage examples](#usageExamples)

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

    * ```CGO_ENABLED=0 GOOS=windows GOARCH=amd64 go build .```
* MacOS:

    * ```CGO_ENABLED=0 GOOS=darwin GOARCH=amd64 go build .```
* Linux-based OS:

    * ```CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build .```

Note: If your CPU architecture is not x86_64 but instead ARM, set ```GOARCH=arm64```.

These commands will create a binary called gophy in the current directory. It is recommended to statically build gophy so that it could also be run in a minimal scratch docker container.

<a name="buildingGophy"/>

### Building cbmroot
This step is only necessary when you want to run a miner that actively works on simulation tasks and it can be skipped when you run gophy as a normal full or light node.

[CbmRoot](https://git.cbm.gsi.de/computing/cbmroot) is the analysis and simulation framework used by the CBM experiment at FAIR in Darmstadt. In order to use cbmroot you first need to acquire required external software that is contained in the [FairSoft](https://github.com/FairRootGroup/FairSoft) repository. You can follow the linked official resources to build the software environment or if you are using Ubuntu 24.04 LTS you can also use my own [simplified instructions](https://github.com/felix314159/gophy/blob/main/tutorials/ubuntu24.04_cbmsoft-cbmroot_installationInstructions.pdf).
<a name="buildingCbmroot"/>

## Docker setup
This section can be skipped for normal usage of gophy (there is no need to run it in Docker). However, it is recommended to use Docker when you want to run many nodes on the same machine.
First install Docker Engine as describe in the [official instructions](https://docs.docker.com/engine/install/). Docker Desktop is not needed here, so just choose your OS from list of supported platforms and follow the instructions for Docker Engine. Then follow the [post-install instructions](https://docs.docker.com/engine/install/linux-postinstall/) so that you can run Docker without root privileges.

In order to run many gophy nodes locally to test their performance under controlled but adjustable networking conditions (more details about the usage of tc-netem will follow), navigate to the docker folder of this repo and perform the following steps. These steps assume you are using some Linux-distro:
* 0. Statically compile gophy and place the binary in the docker folder of this repo. Then navigate your terminal to that folder.
* 1. Build image:
    * ```docker build -t gophyimage .```
* 2. Run multiple nodes (3x light node + 3x full node + 1x Root Authority):
    * ```chmod +x runNodes.sh```
    * ```./runNodes.sh```
* 3. Stop all nodes
    * ```docker stop $(docker ps -a -q)```

More useful docker commands can be found in my [docker-tutorial](https://github.com/felix314159/gophy/blob/main/tutorials/docker-tutorial-linux-githubVersion.md). The sh script used was created to test syncing nodes from scratch under different networking conditions that are artificially introduced using tc-netem. For this reason, the script configures the nodes to fully reset and create a new identity each time they are started.
<a name="docker"/>

## Usage / Running gophy
This section goes into detail about which flags you can use to affect the behavior of gophy, how monitoring of multiple nodes works and provides gophy usage examples with explanations.
<a name="usage"/>

### Flags
The following command-line flags are currently supported:
| Flag    | Default value | Description |
| -------- | ------- | ------- |
| topicNames | pouw_chaindb,pouw_newProblem,<br>pouw_newBlock,pouw_minerCommitments,<br>pouw_transactions,pouw_raSecretReveal | Choose which [PubSub](https://docs.libp2p.io/concepts/pubsub/overview/) topics to subscribe to |
| syncMode | SyncMode_Initial_Full | Defines which kind of node will be run and affects its behavior. |
| localKeyFile | false | When set to true, start the node and re-use an existing private key (which is the node's identity). When set to false, a new identity (Ed25519 key) will be created and stored in OpenSSH format as privkey.key. |
| pw | insecurepassword | Sets the password that is used to encrypt the private key before it is written in OpenSSH format as privkey.key. It is recommended to set a secure password when running a node in production. Gophy will warn you when you do not set a different password than the default. |
| httpPort | 8087 | Sets the port for the locally run HTTP server used by nodes for sending transactions (localhost:8087/send-transaction) and used by RA to define and publish new block problems (localhost:8087/send-simtask). |
| dockerAlias | mynode | Sets the temporary alias (name) of a node so that it is easy to tell nodes apart when running a large number of them on the same machine using Docker and observing their actions via the monitoring at localhost:12345. |
| dump | nil | Allows the user to dump information of any locally stored block. You can either specify the hash of the block that should be dumped or use special values 'latest' or 'genesis' to dump the latest or oldest block information. This command does not start the node. |
| raMode | false | This flag is used to make the node behave as the Root Authority. This only works when you have access to the private key of the RA because each message sent must be signed and other nodes know the public key of the RA. |
| raReset | false | This flag is used during testing/development to start the RA after resetting all blockchain data to genesis. |

You can also invoke gophy with ```./gophy --help``` to get a brief explanation of each flag. For more information about all possible values for each flag and their effects be sure to check out the documentation.
<a name="flags"/>

### Dashboard
A lightweight monitoring solution was written from scratch so that the activities of multiple nodes running e.g. via Docker can be easily observed. Gophy runs a simple HTTP server at localhost:12345 that locally run nodes send information about what they are currently doing to. The server is only started when it is not already running, so there is no issue when many nodes are started at once using Docker. The dashboard is viewed via the browser, updates in real-time and separates information of each node by their dockerAlias string. Each message sent by a node to the dashboard contains information about the current timestamp, what kind of event occurred (e.g. connected to new node) and a message field that can hold an arbitrary string to provide additional information.
<a name="dashboard"/>

### Usage examples
Miner examples:
* Create a new miner node identity and sync to the network for the first time. Then actively work on block problems to earn tokens (requires running node in cbmroot environment). Encrypt your identity file (private key) with a custom password:
    *  ```./gophy -syncMode=SyncMode_Initial_Mine -httpPort=8098 -dockerAlias=miner1 -pw=mysuperstrongpassword```
* You had a miner node but you stopped running it. Now continue running the same miner (using same identity and preserving data that was already locally stored). This command fails if the provided password is wrong:
    *   ```./gophy -syncMode=SyncMode_Continuous_Mine -httpPort=8098 -dockerAlias=miner1 -localKeyFile=true -pw=mysuperstrongpassword```

---

Full node examples:
* Create a new full node identity and sync to the network for the first time. Then help other nodes to sync but do not try to work on block problems (does not require cbmroot environment):
    *  ```./gophy -syncMode=SyncMode_Initial_Full -httpPort=8098 -dockerAlias=fullnode1```
* You had a full node but you stopped running it. Now continue running the same full node (using same identity and preserving data that was already locally stored):
    *   ```./gophy -syncMode=SyncMode_Continuous_Full -httpPort=8098 -dockerAlias=fullnode1 -localKeyFile=true```

---

Light node examples:
* Create a new light node identity and sync to the network for the first time. Then help other nodes to sync whenever possible but do not try to work on block problems (does not require cbmroot environment):
    *  ```./gophy -syncMode=SyncMode_Initial_Light -httpPort=8098 -dockerAlias=lightnode1```
* You had a light node but you stopped running it. Now continue running the same light node (using same identity and preserving data that was already locally stored):
    *   ```./gophy -syncMode=SyncMode_Continuous_Light -httpPort=8098 -dockerAlias=lightnode1 -localKeyFile=true```

---

Root Authority example:
* Run your node as the Root Authority and start a new blockchain from scratch. Requires access to the file "raprivkey.key" and the password it was encrypted with:
    *  ```./gophy -syncMode=SyncMode_Continuous_Full -localKeyFile=true -raMode=true -raReset=true -pw=thisisnottheactualrapw -dockerAlias=ra```

---

Generally, the "Initial" means that all local data is deleted to start a new node from scratch. "Continuous" means try to re-use locally existing data and continue from there. Both "Initial" and "Continuous" mode nodes will perform an initial sync, but there is a slight difference: The "Initial" mode nodes have deleted all local data so they must request all data from other nodes, "Continuous" nodes however first ask the node network which block hashes exist to determine how much new data they need and then they only request the data that currently is locally missing. 

Finally, the difference between "Light", "Full" and "Mine" syncModes is what the final state will be: By choosing light you will end up with a light node, with full you will end up with a full node that does not try to solve block problems and with mine you will end up with a full node that does try to solve block problems. So the syncMode not only affects the behavior of the node during the initial sync phase but also the node behavior after having completed its initial sync.

<a name="usageExamples"/>

## Contribution
Gophy is in active developement and any form of feedback or improvement suggestions are welcome. If you have questions about the blockchain architecture itself you can ask in the [Discussions](https://github.com/felix314159/gophy/discussions) tab that was set up. If you want to help improve the code itself feel free to fork and send pull requests, but ensure to document your code with pkgsite-compatible comments so that the documentation can automatically stay up-to-date. You can also [report any issues](https://github.com/felix314159/gophy/issues) or concerns you might have.
<a name="contribution"/>

## Documentation
The code makes use of pkgsite-compatible comments used to automatically generate a documentation that can be accessed via the browser. In order to locally host the documentation read throught my [pkgsite tutorial](https://github.com/felix314159/gophy/blob/main/tutorials/pkgsite_documentation_tutorial.md). An online version of the documentation will also soon be available.

The documentation contains more detailed information than this repository and should be used as a reference for users and developers. Not only does it make it easier to understand the existing codebase but it can also be utilized by users to learn more e.g. about differences between the different kinds of nodes gophy supports.
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
