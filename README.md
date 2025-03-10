# Gophy
Gophy is p2p command-line application written from scratch in Go to implement and test ideas for a novel blockchain network that is based on the idea of useful work. Its goal is to provide anyone the ability to donate computational power to a real-world High Energy Physics (HEP) experiment by running Monte Carlo simulation tasks sent out by a privileged node called the Root Authority (RA). The RA is a node that will be run by a representative of the experiment to ensure the 'usefulness' aspect of tasks. Simplified, the RA sends out simulation tasks and receives the resulting data from miners. This data then is locally stored by the RA so that it can make use of it.

Disclaimer: Gophy is still in active development and the raprivkey.key used here for demonstration purposes will not be used in production. Bugs or errors may occur and I am not liable for any damage that might be inflicted to your system by running this application. Before running this application for testing purposes, change the RendezvousString in constants.go to any string of your choice (to separate your test network from others), create a new raprivkey (I wrote [this Go script](https://github.com/felix314159/libp2p-mineNodeID) to make this easier) and replace the existing raprivkey from the root folder and from the docker folder with the new key. Finally, update the value RANodeID in constant.go with the new RA node ID. These steps ensure that your tests are not affected by someone using the publicly known example RA privkey. 

![alt text](https://github.com/felix314159/gophy/blob/main/tutorials/exampleOutput/gophyExampleOutput.png?raw=true "Gophy Example Output")

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

* [Literature](#literature)

* [Work in progress](#wip)

* [Credits](#credits)

* [License](#license)

## Features
<a id="features"></a>

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
    * Ed25519 is used to sign messages so that node identities can not be spoofed. The libp2p nodeID is derived from the public key (and you can derive a node's public key from its libp2p nodeID), the public key is derived from the private key. As long your private key is kept secret no one should be able to impersonate you.
    * Encrypted communication channels can be used for secure communication between nodes, see [Noise](https://docs.libp2p.io/concepts/secure-comm/noise/) for more information.
* Novel blockchain architecture
    * In order to guarantee the usefulness of simulation tasks, a privileged node called the Root Authority (RA) define and send out new block problems in regular intervals. The RA is supposed to be controlled by a representative of the HEP experiment that profits from the simulation data the network generates. To mitigate some undeseriable side-affects of an increased degree of centralization, the blockchain architecture is designed in a way that nodes can control the RA which limits its power. However, it must be noted that in its current form the RA is both a necesssary evil (to preserve usefulness of tasks) and a single point of failure (no more block problems = no more blocks).
    * Gophy also features a novel winner selection algorithm named DFTWS. Theoretical details about DFTWS are explained [here](https://arxiv.org/abs/2312.01951) and a code repository to analyze its fairness can be found [here](https://github.com/felix314159/dftws-fairnessEvaluation).
* HTTP API for sending transactions and new simulation tasks (default port 8087 can be overwritten via flag)
    * Gophy runs a local http server which provides a user-friendly way for nodes to broadcast transactions by visiting localhost:8087/send-transaction
    * It also provides a user-friendly way for the RA to broadcast a new block problem (simulation task) to the node network by visiting localhost:8087/send-simtask
* Self-built monitoring solution to observe many locally ran instances of gophy
    * When running many nodes on the same machine via e.g. Docker you can get an overview of what each node is doing by visiting localhost:12345
* For developers: functions to rapidly simulate blockchain growth over time
    * Pseudo.go contains functions that can be used to generate a valid blockchain of specified size (e.g. 200 blocks with each block containing 30 transactions) to observe not only how the size of the blockchain itself grows over time but also to be able to test the sync between nodes with a large blockchain from the start. In order to be able to generate valid transactions, the currently published version of gophy contains a pre-allocation of 30k tokens for the RA. This ensures that the RA is able to 'send' many pseudo-randomly transactions in pseudo.go so that the generated blocks are valid.

## Build instructions
<a id="building"></a>

In order to build gophy you must install Golang. Follow official [Golang installation instructions](https://go.dev/doc/install) before continuing.

### Building gophy
<a id="buildingGophy"></a>

First download gophy using:
* ```git clone https://github.com/felix314159/gophy.git```

then navigate to the folder:
* ```cd gophy```

then download dependencies:
* ```go mod tidy```

then in order to statically build the gophy binary use one of the following commands depending on your OS:
* Windows:

    * ```CGO_ENABLED=0 GOOS=windows GOARCH=amd64 go build .```
* MacOS (x64):

    * ```CGO_ENABLED=0 GOOS=darwin GOARCH=amd64 go build .```
* MacOS (Apple Silicon - ARM):

    * ```CGO_ENABLED=0 GOOS=darwin GOARCH=arm64 go build .```
* Linux-based OS:

    * ```CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build .```

Note: If your CPU architecture is not x86_64 but instead ARM, set ```GOARCH=arm64```.

These commands will create a binary called gophy in the current directory. It is recommended to statically build gophy so that it could also be run in a minimal scratch docker container.


### Building cbmroot
<a id="buildingCbmroot"></a>

This step is only necessary when you want to run a miner that actively works on simulation tasks and it can be skipped when you run gophy as a normal full or light node.

[CbmRoot](https://git.cbm.gsi.de/computing/cbmroot) is the analysis and simulation framework used by the CBM experiment at FAIR in Darmstadt. In order to use cbmroot you first need to acquire required external software that is contained in the [FairSoft](https://github.com/FairRootGroup/FairSoft) repository. You can follow the linked official resources to build the software environment or if you are using Ubuntu 24.04 LTS you can also use my own [simplified instructions](https://github.com/felix314159/gophy/blob/main/tutorials/ubuntu24.04_cbmsoft-cbmroot_installationInstructions.pdf).


## Docker setup
<a id="docker"></a>

This section can be skipped for normal usage of gophy (there is no need to run it in Docker). However, it is recommended to use Docker when you want to run many nodes on the same machine.
First install Docker Engine as describe in the [official instructions](https://docs.docker.com/engine/install/). Docker Desktop is not needed here, so just choose your OS from list of supported platforms and follow the instructions for Docker Engine. Then follow the [post-install instructions](https://docs.docker.com/engine/install/linux-postinstall/) so that you can run Docker without root privileges.

In order to run many gophy nodes locally to test their performance under controlled but adjustable networking conditions (more details about the usage of tc-netem can be found in [this tutorial](https://github.com/felix314159/gophy/blob/main/tutorials/tcNetemTutorial.md) I wrote), navigate to the docker folder of this repo and perform the following steps. These steps assume you are using some Linux-distro:
* 0. Statically compile gophy and place the binary in the docker folder of this repo. Then navigate your terminal to that folder.
* 1. Build image:
    * ```docker build -t gophyimage .```
* 2. Run multiple nodes (3x light node + 3x full node + 1x Root Authority):
    * ```chmod +x runNodes.sh```
    * ```./runNodes.sh```
* 3. Stop all nodes
    * ```docker stop $(docker ps -a -q)```

More useful docker commands can be found in my [docker-tutorial](https://github.com/felix314159/gophy/blob/main/tutorials/docker-tutorial-linux-githubVersion.md). The sh script used was created to test syncing nodes from scratch under different networking conditions that are artificially introduced using tc-netem. For this reason, the script configures the nodes to fully reset and create a new identity each time they are started.


## Usage / Running gophy
<a id="usage"></a>

This section goes into detail about which flags you can use to affect the behavior of gophy, how monitoring of multiple nodes works and provides gophy usage examples with explanations.

### Flags
<a id="flags"></a>

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
| dumpState | false | Allows a full node to dump the statedb in a human-readable format. This makes it possible to view the Balance and Nonce of all nodes. This flag can not be used by light nodes because they do not locally build the state. |
| raMode | false | This flag is used to make the node behave as the Root Authority. This only works when you have access to the private key of the RA because each message sent must be signed and other nodes know the public key of the RA. |
| raReset | false | This flag is used during testing/development to start the RA after resetting all blockchain data to genesis. |

You can also invoke gophy with ```./gophy -help``` to get a brief explanation of each flag. For more information about all possible values for each flag and their effects be sure to check out the documentation.


### Dashboard
<a id="dashboard"></a>

A lightweight monitoring solution was written from scratch so that the activities of multiple nodes running e.g. via Docker can be easily observed. Gophy runs a simple HTTP server at localhost:12345 that locally run nodes send information about what they are currently doing to. The server is only started when it is not already running, so there is no issue when many nodes are started at once using Docker. The dashboard is viewed via the browser, updates in real-time and separates information of each node by their dockerAlias string. Each message sent by a node to the dashboard contains information about the current timestamp, what kind of event occurred (e.g. connected to new node) and a message field that can hold an arbitrary string to provide additional information. The performance stats are added and shown in real-time, but I also provide an [example output](https://github.com/felix314159/gophy/blob/main/tutorials/exampleOutput/monitoringOutputExampleExport.pdf) after a completed test to show what the monitoring website might look like after some time. It is admittedly not very human-readable in its current form, but it is functional and a great help for developing and debugging. In order to make this data human-readable, timestamps are entered into an [Apple Numbers document](https://github.com/felix314159/gophy/blob/main/tutorials/exampleOutput/monitoringOutputExampleAnalysis.numbers) I have created with simple formulae so that the amount of time in seconds certain steps take easily be compared between different nodes. The test scenario is as follows: A pseudo blockchain is created (e.g. 100 blocks with each block holding 100 transactions) using pseudo.go. Then 2 miners and 7 docker nodes (3 full + 3 light + 1 RA) are started and I wait until everyone completed their sync (only the RA continues the existing pseudo blockchain, the other nodes start from scratch). Then I wait until a new block is mined and accepted by everyone to verify that they really are in sync (otherwise they would deny the block and panic). Then I start two more nodes (7f = 1 full, 8l = 1 light) and let them sync to the network to have a more realistic measurement (in production almost no one would startup at the same time as the RA, instead it would be expected that you are joining a large existing network of nodes). As soon as these last two nodes have joined the network I stop the test and enter the resulting data into the numbers document to analyze it. Note: For this test the docker runNodes.sh script must be slightly adjusted so that raReset is set to false, otherwise it would not continue with the given pseudo blockchain.

### Usage examples
<a id="usageExamples"></a>

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
* Run your node as the Root Authority and start a new blockchain from scratch. Requires access to the file "raprivkey.key" and the password it was encrypted with (provided here):
    *  ```./gophy -syncMode=SyncMode_Continuous_Full -localKeyFile=true -raMode=true -raReset=true -pw=supersecretrapw -dockerAlias=ra```

---

Generally, the "Initial" means that all local data is deleted to start a new node from scratch. "Continuous" means try to re-use locally existing data and continue from there. Both "Initial" and "Continuous" mode nodes will perform an initial sync, but there is a slight difference: The "Initial" mode nodes have deleted all local data so they must request all data from other nodes, "Continuous" nodes however first ask the node network which block hashes exist to determine how much new data they need and then they only request the data that currently is locally missing. 

Finally, the difference between "Light", "Full" and "Mine" syncModes is what the final state will be: By choosing light you will end up with a light node, with full you will end up with a full node that does not try to solve block problems and with mine you will end up with a full node that does try to solve block problems. So the syncMode not only affects the behavior of the node during the initial sync phase but also the node behavior after having completed its initial sync.

## Documentation
<a id="documentation"></a>

The code makes use of pkgsite-compatible comments used to automatically generate a documentation that can be accessed via the browser. You can find the always up-to-date [online documentation here](https://pkg.go.dev/github.com/felix314159/gophy#section-directories).

The documentation contains more detailed information than this repository and should be used as a reference for users and developers. Note: It is also possible to locally host the documentation on your machine, for a simplified guide read through my [pkgsite tutorial](https://github.com/felix314159/gophy/blob/main/tutorials/pkgsite_documentation_tutorial.md).


## Literature
<a id="literature"></a>

For more information about Proof-of-Useful-Work and its challenges compared to traditional hash-based Proof-of-Work check out [Challenges of Proof-of-Useful-Work (PoUW)](https://ieeexplore.ieee.org/document/10087185).
The blockchain architecture behind gophy is described in more detail in this [peer-reviewed publication](https://ieeexplore.ieee.org/document/10844433) and in this [Arxiv preprint](https://arxiv.org/abs/2404.09093).
The fair and transparent winner selection algorithm used for selecting block winners in gophy is described in more detail in the open-access ACM Distributed Ledger Technologies Journal: Research and Practice publication [DFTWS](https://dl.acm.org/doi/10.1145/3721139). Code to analyze the fairness of DFTWS can be found [here](https://github.com/felix314159/dftws-fairnessEvaluation).
The most comprehensive description and analysis of gophy will be my dissertation that can be expected to be published late 2024/early 2025.


## Contribution
<a id="contribution"></a>

Gophy is in active developement and any form of feedback or improvement suggestions are welcome. If you have questions about the blockchain architecture itself you can ask in the [Discussions](https://github.com/felix314159/gophy/discussions) tab that was set up. If you want to help improve the code itself feel free to fork and send pull requests, but ensure to document your code with pkgsite-compatible comments so that the documentation can automatically stay up-to-date. You can also [report any issues](https://github.com/felix314159/gophy/issues) or concerns you might have.


## WIP (Work in progress)
<a id="wip"></a>

Gophy still needs to be improved before being used in production. The following aspects could still be improved:
* Libp2p-related: [Improve success rate of holepunching](https://discuss.libp2p.io/t/issue-with-holepunching-simple-example/2341). In certain networks gophy might not be able to receive PubSub messages from nodes in other networks, even though the node seems to be publicly available via tools like [libp2p-lookup](https://github.com/mxinden/libp2p-lookup) or [vole](https://github.com/ipfs-shipyard/vole).
* Implement sub-solution matching as described in [this publication](https://arxiv.org/abs/2404.09093). The reason it currently is not implemented is more of a user interface problem: The RA needs an automated workflow that allows it, during the process of defining a set of new simulation tasks, to pre-calulate certain sub-solutions that will be used as decoy when probabilistically verifying the correctness of received solution data. It needs to be easy-to-use and highly automated to make it practical. Until it is implemented, the accepted solution will be chosen only by determining the most common solution of miners.
* Node Identity management: In order to prevent Sybil attacks that try to influence what the most common solution is, every node should in some way prove its real-world identity to the RA before being able to join the network. This would make it more difficult to perform such an attack and it would also make it possible to extend gophy with a reputation-based system in which nodes that do not act according to rules can be punished to disincentivize malicious behavior. The reason this is currently not implemented is that this mainly is not a programming-related challenge but instead would require real-world processes in-place.
* Less reliance on availability of RA: Currently, when RA shuts down its node no new blocks can be created because a block is only created when the block problem is solved but currently only the RA can submit new block problems. An idea would be to implement a fallback mechanism in which nodes agree which simulation tasks to run when the RA is not available. The usefulness aspect of solutions would be temporarily lost in such a case, but at least there would be a way to continuously produce new blocks (that contain transactions) during RA outages so that not everything comes to a halt.
* Mitigate the impact of spam attacks: It currently is unclear how well the network would hold up against various kinds of spam attacks. Since every change to gophy would make this topic relevant again, it should be taken care of before going into production.
* More flexibility for RA to define simulation tasks: Currently, the RA is able to define various parameters that will be passed to an otherwise static example simulation script. In the future, the block problem itself should contain the C script that defines the simulation task to give the RA more flexibility with regards to which block problems it can define and send out. The reason this currently is not implemented already is that this will also require the RA to know beforehand which files of interest will be created during the simulation so that it can instruct the miners to send these files back. Currently, it is known that no matter which parameters are provided just a few files with the same filename will result from the simulation which made it initially easier to define structs and functions for automated reading of resulting simulation files from the filesystem before they are serialized and sent back to the RA.
* Silence two known warnings:
    * ```websocket: failed to close network connection```: It is a harmless warning that comes from go-libp2p which does not negatively impact gophy but it would be nice to be able to silence it.
    * ```quic buffer size warning```: This warning is shown in some linux distros and does not negatively impact the performance of gophy. I already implemented an automated fix, however it requires root to perform and I do not think it is worth forcing root privileges just to silence a harmless warning.


## Credits
<a id="credits"></a>

I would like to thank FIAS and HFHF for supporting my work. I would also like to thank my professor who provided valuable feedback over time.

Additionally, I want to give credits to the authors of the following Go packages, while I aimed to make use of Go's standard library or write my own code, in certain instances I relied on third-party code or on external packages that are developed outside of the Go core:
* [Go-libp2p](https://github.com/libp2p/go-libp2p) which is the networking stack used for peer discovery and data exchange between peers.
* [Bbolt](https://github.com/etcd-io/bbolt) which is the database used to store blockchain data.
* [Msgpack](https://github.com/vmihailenco/msgpack) which is a Go implementation of the MessagePack object serialization library.
* [Base58](https://github.com/btcsuite/btcd/tree/master/btcutil/base58) which is a Go implementation of the base58 plain text encoding scheme.
  
* [Crypto/SHA3](https://pkg.go.dev/golang.org/x/crypto/sha3) which is a Go implementation of SHA3 functions.
* [Crypto/SSH](https://pkg.go.dev/golang.org/x/crypto/ssh) which contains functions to serialize Ed25519 and other kinds of keys into the widely supported OpenSSH format.


## License
<a id="license"></a>

Gophy is licensed under the MIT license. For more information see [MIT license](https://opensource.org/license/MIT).

