package database

// topichandlers.go contains functions that are run when new messages on the various topics are received or when a message should be send to a topic.

import (
	"context"
	"encoding/hex"
	"fmt"
	"time"

	//"strings"

	// libp2p stuff
	pubsub "github.com/libp2p/go-libp2p-pubsub"
	"github.com/libp2p/go-libp2p/core/host"
	"github.com/libp2p/go-libp2p/core/peer"

	"github.com/vmihailenco/msgpack/v5"

	"github.com/felix314159/gophy/block"
	"github.com/felix314159/gophy/block/hash"
	"github.com/felix314159/gophy/block/simsol"
	"github.com/felix314159/gophy/block/transaction"
	"github.com/felix314159/gophy/logger"
	"github.com/felix314159/gophy/monitoring"
)

// ---- Function used to send a message to a specific topic ----

// TopicSendMessage is used to automatically (as in no user input from console required) send messages to a pubsub topic.
// Possible target topics are:
//		pouw_chaindb			- Used to request chaindb data from other nodes (e.g. when newly joining the network) [replies are sent via direct chat, not via this topic]
//		pouw_minerCommitments	- Used by miners to broadcast cryptographic commitments and signed messages to prove to the network that they knew the solution before the problem expired without revealing it.
//		pouw_newProblem			- Only RA should send to this topic to broadcast new block problems (if other nodes send messages to this topic they will be ignored) [solutions are sent directly to the RA, not to this topic]
//		pouw_newBlock			- Only RA should send to this topic to broadcast new blocks (if other nodes send messages to this topic they will be ignored)
func TopicSendMessage(ctx context.Context, targetTopicString string, msgContent []byte) error {

	// convert pouw_chaindb string to Topic object
	var targetTopicPtr *pubsub.Topic
	for _, topicPtr := range PubsubTopicSlice {			// loop over all topics
		if (*topicPtr).String() == targetTopicString {	// if targetTopicString topic is found get this topic object
			targetTopicPtr = topicPtr
			break
		}
	}

	// check whether user-given Topic is subscribed to
	if targetTopicPtr == nil {
		logger.L.Printf("ERROR: User tried to send msg to a non-subscribed topic.")
		// remind user which topics he is subscribed to
		logger.L.Printf("You are currently subscribed to the following topics:")
		for _, subbedTopicPtr := range PubsubTopicSlice {
			logger.L.Printf("\t%v", (*subbedTopicPtr).String())
		}
		// you should only be allowed to send messages to topics you are subscribed to because in order to see whether the message was actually successfully sent to the topic you might e.g. listen until u get ur own message. this only works when u are subscribed to the topic u are sending to.
		return fmt.Errorf("TopicSendMessage - User tried to send msg to a non-subscribed topic: %v \n", targetTopicString)
	}

	// send input to specified channel (must be cast to []byte)
	err := targetTopicPtr.Publish(ctx, msgContent) // defined in line 224 of https://github.com/libp2p/go-libp2p-pubsub/blob/master/topic.go
	if err != nil {
		return fmt.Errorf("TopicSendMessage - Failed to send message due to error: %v \n", err)
	}

	return nil
	
}

// ---- Functions that are called when topic message is received ----

// TopicReceiveMessage is responsible for calling a suitable topic handler function whenever a message is received on any subscribed topic.
func TopicReceiveMessage(ctx context.Context, sub *pubsub.Subscription, h host.Host) {
	for {
		m, err := sub.Next(ctx)		// returns next *Message in subscription (Message defined here https://github.com/libp2p/go-libp2p-pubsub/blob/master/pubsub.go#L229)
		if err != nil {
			logger.L.Panic(err)
		}

		t := sub.Topic()	// line 20 of (https://github.com/libp2p/go-libp2p-pubsub/blob/master/subscription.go), returns string

		// do not handle received messages that you sent yourself, EXCEPTIONS: miners take note of their own commitments so that they can perform the solution determination + winner selection
		// but such a case at least confirms that the topic broadcast was successful. everyone also listens to their own transactions so that they are added to the pending transactions slice
		if (h.ID() == m.ReceivedFrom && t != "pouw_minerCommitments" && t != "pouw_transactions") {		// https://github.com/libp2p/go-libp2p/blob/master/core/host/host.go#L26
			logger.L.Printf("Message to topic %v has successfully been sent.\n", t) // msg was received, so it must have been sent successfully
			continue
		}

		logger.L.Printf("Topic: %v - Received data from %v.\n", t, m.ReceivedFrom) // no need to print Sender either as its always RA if accepted or it will be declined anyways

		// depending on which topic msg was received from call responsible function to handle content
		// these calls should be handled in goroutines so use go keyword
		if t == "pouw_chaindb" {
			go TopicChaindbReceiveEvent(*m, h, ctx)
		} else if t == "pouw_newProblem" {
			go TopicNewProblemReceiveEvent(*m, h, ctx)
		} else if t == "pouw_newBlock" {
			go TopicNewBlockReceiveEvent(*m, h, ctx)
		} else if t == "pouw_minerCommitments" {
			go TopicNewCommitmentReceiveEvent(*m, h, ctx)
		} else if t == "pouw_transactions" {
			go TopicNewTransactionReceiveEvent(*m, h, ctx)
		} else if t == "pouw_raSecretReveal" {
			go TopicNewRASecretRevealEvent(*m, h, ctx)
		} else {
			logger.L.Printf("Received data for unknown topic: %v\nIgnoring it.\n", t)
		}

		time.Sleep(10 * time.Millisecond)

	}
}

// TopicNewBlockReceiveEvent is a topic-specific function that is triggered when a node receives a message in topic "pouw_newBlock".
// The topic is used by the RA to send out new chaindb blocks to the network. Nodes will ignore any message sent to this topic,
// if its signature is not that of the RA.
func TopicNewBlockReceiveEvent(m pubsub.Message, h host.Host, ctx context.Context) {
	// while in Initial you ignore these topic messages (as you are not in sync yet so there is no way to stay in sync)
	curSyncMode := SyncHelper.NodeModeGet()
	if  (curSyncMode == SyncMode_Initial_Full) || (curSyncMode == SyncMode_Initial_Light) || (curSyncMode == SyncMode_Initial_Mine){
		return
	}

	// ----

	// determine nodeID of sender (might or might not be the ORIGINAL sender)
	senderNodeID := m.ReceivedFrom.String()

	// access data sent
	data := string(m.Data)

	// we ONLY care about the message if the message was signed BY THE RA and the signature is valid
	// 		check whether data is valid and can be deserialized
	dataBytes := []byte(data)
	recData, _, err := TransportStructExtraction(TSData_Block, dataBytes, true) // if sig cant be verified with RA pubkey it either was not originally sent by RA or the data was corrupted or manipulated during transport
	if err != nil {
		logger.L.Printf("Data received from %v (who might or might not be the original sender) was declined for the following reason: %v \n", senderNodeID, err)
		return
	}

	// get latest chaindb block/header as bytes
	prevBlockBytes, err := BlockGetBytesFromDb("latest")
	if err != nil {
		logger.L.Panic(err)
	}

	// check validity (pass info on whether you are a full node or a light node)
	logger.L.Printf("Received new block from RA. Will now check whether it passes validity tests.")
	err = BlockVerifyValidity(IAmFullNode, prevBlockBytes, recData, true)
	if err != nil {
		logger.L.Panicf("Block received from %v (orginally created by RA) is not a valid continuation of your blockchain! Resync your node! Reason: %v \n", senderNodeID, err)
	}

	// ---- Verify that you agree with RA on how anything related to winner selection was performed ----

	// 0. before RA even should send a new block two things must already have happened:
	//			0.1 RA has revealed its secret bytes
	raCommitSec := BlockProblemHelper.GetRAcommitSecret()
	if raCommitSec == "" {
		logger.L.Printf("I received a block from RA but I had not yet received the raCommitSecret! Since the received block also contains the raSecret I can recover from this situation..\n")
		
		// extract raCommit from received block
		recBlockFull := FullBlockBytesToBlock(recData)
		
		// ensure it has expected length
		raCommitSec = recBlockFull.SimulationTask.SimHeader.RAcommit.GetString()

		// these secret bytes can never be less than 7 chars, so panic if sth unexpected occurrs (later performance stat reporting assumes the first 7 chars of this exist)
		// 7 was arbitrarily chosen, a bit like Github allows you to specify a commit by just giving the first 7 hex chars of the commit hash
		if len(raCommitSec) < 7 {
			logger.L.Panicf("Received secret RA commit bytes but they are too short! Expected sth longer than 7 chars but got: %v\nI will panic!", raCommitSec)
		}

		BlockProblemHelper.SetRAcommitSecret(raCommitSec)

		// ---

		// report performance stat that the RA secretBytes commitment was received (Event RACommitRevealed)
		// 		get current time
		curTimeRightNow := time.Now().UnixNano()
		// 		msgRAcommit should only show first 7 chars (otherwise it's the longest reported message and might lead to ugly linebreaks)
		msgRAcommit := fmt.Sprintf("RA Commit Reveal (first 7 chars): %v", raCommitSec[:7])
		//		construct stat to be sent
		pStat := monitoring.NewPerformanceData(curTimeRightNow, MyDockerAlias, monitoring.Event_RACommitRevealed, msgRAcommit)
		// 		post stat
		err = monitoring.SendPerformanceStatWithRetries(pStat)
		if err != nil {
			logger.L.Printf("Warning - Failed to report performance stat: %v", err)
		}

		// ---

	}

	//			0.2 Current block problem must have expired
	currentTime := block.GetCurrentTime()
	expirationTime := BlockProblemHelper.GetProblemExpirationTime()
	currentProblemID := BlockProblemHelper.GetProblemID().GetString()
	if currentTime < expirationTime {
		logger.L.Panicf("I received a block from RA but the current block problem has not expired yet! Current time is %v and active problem with ID %v will expire at %v which is in the future. I will panic!", currentTime, currentProblemID, expirationTime) 
	}

	// 1. determine which solutionHash was accepted by RA
	//		deserialize data into block
	recBlockFull := FullBlockBytesToBlock(recData)
	//		accepted solutionHash (until now a node that is not a miner might only know H(solutionHash) from other miner's commitments but not solutionHash itself)
	acceptedSolutionHash := recBlockFull.BlockHeader.BlockWinner.SolutionHash.GetString()

	// 2. determine eligible winner nodes one of which later will be chosen block winner
	eligibleActiveMiners := BlockProblemHelper.DetermineEligibleMiners(acceptedSolutionHash)
	amountEligibleMiners := len(eligibleActiveMiners)

	var winnerNodeAddress string

	if amountEligibleMiners > 1 {
		// perform non-trivial winner selection
		winnerNodeAddress = WinnerSelection(eligibleActiveMiners, raCommitSec)
		logger.L.Printf("I selected winner node: %v\n", winnerNodeAddress)

	} else if amountEligibleMiners == 1 {
		// winner has already been determined
		winnerNodeAddress = eligibleActiveMiners[0].Commitment.OriginalSenderNodeID
		logger.L.Printf("I have determined that winner selection is trivial because only one miner is eligible. Winner: %v\n", winnerNodeAddress)
	} else { // it is impossible that you determine no node should be winner because then RA would not have created a block in the first place
		//winnerNodeAddress = ""
		logger.L.Panicf("I have determined that no miner is eligible for winner selection. But this contradicts with RA sending me a new block! I will panic!\n")
	}

	// ensure that you have chosen the same block winner as RA
	if recBlockFull.BlockHeader.BlockWinner.WinnerAddress != winnerNodeAddress {
		logger.L.Panicf("I have determined that the block winner should be %v but RA has determined that block winner should be %v. I will panic!", winnerNodeAddress, recBlockFull.BlockHeader.BlockWinner.WinnerAddress)
	}

	// ---

	// report performance stat that you have determined the block winner and you are in agreement with who RA selected
	// 		get current time
	curTimeRightNow := time.Now().UnixNano()
	//		get msg
	msg := fmt.Sprintf("Selected winner: %v", winnerNodeAddress)
	//		construct stat to be sent
	pStatWinner := monitoring.NewPerformanceData(curTimeRightNow, MyDockerAlias, monitoring.Event_BlockWinnerFound, msg)
	// 		post stat
	err = monitoring.SendPerformanceStatWithRetries(pStatWinner)
	if err != nil {
		logger.L.Printf("Warning - Failed to report performance stat: %v", err)
	}

	// ---

	// ---- Store received block locally ----

	// remember hash of block as string, so that it can later be reported as performance stat
	var recBlockHashString string

	// write new block/header to chaindb (full node adds full block, light node only adds header)
	if !IAmFullNode { // case: light node
		// serialize header
		newBlkHeaderSer := HeaderToBytes(recBlockFull.BlockHeader)
		// write to chaindb
		err = BlockWriteToDb(newBlkHeaderSer, false)
		if err != nil {
			logger.L.Panic(err)
		}

		// remove included transactions from pending
		PendingTransactionsRemoveThese(recBlockFull.Transactions)

		// remember hash of header
		recBlockHashString = HeaderToHash(recBlockFull.BlockHeader).GetString()

	} else { // case: full node
		err = BlockWriteToDb(recData, true)
		if err != nil {
			logger.L.Panic(err)
		}

		// have block affect state db
		recBlock := FullBlockBytesToBlock(recData)
		_, err := StateDbProcessAndUpdateBlock(recBlock)
		if err != nil {
			logger.L.Panic(err)
		}

		// remove included transactions from pending
		PendingTransactionsRemoveThese(recBlock.Transactions)

		// remember hash of header
		recBlockHashString = HeaderToHash(recBlock.BlockHeader).GetString()

	}

	if IAmFullNode {
		logger.L.Printf("New block from RA has successfully been added to chaindb and it has affected statedb.")
	} else {
		logger.L.Printf("New header from RA has successfully been added to chaindb.")
	}

	// --- 

	// report performance stat that a new valid block was received from RA (Event NewBlockReceived)
	// 		get current time
	curTimeRightNow = time.Now().UnixNano()
	//		get msg
	msg = fmt.Sprintf("Block Hash: %v", recBlockHashString)
	//		construct stat to be sent
	pStat := monitoring.NewPerformanceData(curTimeRightNow, MyDockerAlias, monitoring.Event_NewBlockReceived, msg)
	// 		post stat
	err = monitoring.SendPerformanceStatWithRetries(pStat)
	if err != nil {
		logger.L.Printf("Warning - Failed to report performance stat: %v", err)
	}

	// ---
	
}

// TopicNewRASecretRevealEvent is a topic-specific function that is triggered when a node receives a message in topic "pouw_raSecretReveal".
// This implies that the current block problem has expired, the RA reveals the previously generated random bytes whose hash was already known to all nodes.
// The value is needed to form consensus between nodes on how to choose the block winner from the eligible miners list.
func TopicNewRASecretRevealEvent(m pubsub.Message, h host.Host, ctx context.Context) {
	// ignore these topic messages if you are in an initial syncmode
	curSyncMode := SyncHelper.NodeModeGet()
	if (curSyncMode == SyncMode_Initial_Light) || (curSyncMode == SyncMode_Initial_Full) || (curSyncMode == SyncMode_Initial_Mine) {
		logger.L.Printf("I am in an initial sync mode, that's why I will ignore this data.")
		return
	}
	
	senderNodeID := m.ReceivedFrom.String()

	// access data sent
	data := string(m.Data)
	dataBytes := []byte(data)

	_, raCommitSec, err := TransportStructExtraction(TSData_RAcommitSecret, dataBytes, true) // true because only RA is supposed to be original sender of RAcommitSecret or SimPar via topics; 2nd return parameter: instead of putting the hash, the actual commit string is put as second argument so that it doesnt have to be re-converted from []byte to string later
	if err != nil {
		logger.L.Printf("TopicNewRASecretRevealEvent - Failed to TS extract the received RA commit reveal. It was received from %v (who might or might not be the ORIGINAL sender) and the issue is: %v", senderNodeID, err)
		return
	}

	// ---- RA commit is legit, so now handle it ----

	// these secret bytes can never be less than 7 chars, so panic if sth unexpected occurrs (later performance stat reporting assumes the first 7 chars of this exist)
	// 7 was arbitrarily chosen, a bit like Github allows you to specify a commit by just giving the first 7 hex chars of the commit hash
	if len(raCommitSec) < 7 {
		logger.L.Panicf("Received secret RA commit bytes but they are too short! Expected sth longer than 7 chars but got: %v\nI will panic!", raCommitSec)
	}

	BlockProblemHelper.SetRAcommitSecret(raCommitSec)

	// ---

	// report performance stat that the RA secretBytes commitment was received (Event RACommitRevealed)
	// 		get current time
	curTimeRightNow := time.Now().UnixNano()
	// 		msgRAcommit should only show first 7 chars (otherwise it's the longest reported message and might lead to ugly linebreaks)
	msgRAcommit := fmt.Sprintf("RA Commit Reveal (first 7 chars): %v", raCommitSec[:7])
	//		construct stat to be sent
	pStat := monitoring.NewPerformanceData(curTimeRightNow, MyDockerAlias, monitoring.Event_RACommitRevealed, msgRAcommit)
	// 		post stat
	err = monitoring.SendPerformanceStatWithRetries(pStat)
	if err != nil {
		logger.L.Printf("Warning - Failed to report performance stat: %v", err)
	}


}

// TopicNewProblemReceiveEvent is a topic-specific function that is triggered when a node receives a message in topic "pouw_newProblem".
// The topic is used by the RA for two purposes:
//		1. Sending out new block problem definitions (SimPar)
//		2. Revealing RAcommitSecret which is used for winner selection (RAcommitSecret)
// Nodes will ignore any message sent to this topic, if its signature is not that of the RA.
// Nodes in initial sync mode ignore messages from this topic.
func TopicNewProblemReceiveEvent(m pubsub.Message, h host.Host, ctx context.Context) {
	// ignore these topic messages if you are in an initial syncmode
	curSyncMode := SyncHelper.NodeModeGet()
	if (curSyncMode == SyncMode_Initial_Light) || (curSyncMode == SyncMode_Initial_Full) || (curSyncMode == SyncMode_Initial_Mine) {
		logger.L.Printf("I am in an initial sync mode, that's why I will ignore this data.")
		return
	}
	
	// only RA is allowed to send in this topic, so if the signature can't be verified with RApub then ignore it
	senderNodeID := m.ReceivedFrom.String()
	
	// access data sent
	data := string(m.Data)
	dataBytes := []byte(data)

	recData, _, err := TransportStructExtraction(TSData_SimulationTask, dataBytes, true) // dont care about hash of data
	if err != nil { // signature was probably faked, or RA accidently sent invalid data
		// case: it is now confirmed that SimPar was sent but something went seriously wrong
		logger.L.Printf("TopicNewProblemReceiveEvent - Data received from %v (who might or might not be the ORIGINAL sender) could not be transport struct extracted: %v\n", senderNodeID, err)
		return
	}

	// ---- Ok, so we are a miner and we received a block problem definition from the RA ----

	// it is now safe to deserialize recData into simpar.SimulationTask
	simTaskObject := SimtaskBytesToSimTask(recData)

	// get latest block so that you can do the simtask validity check
	latestBlkSer, err := BlockGetBytesFromDb("latest")
	if err != nil {
		logger.L.Panic(err)
	}
	
	var latestBlkHeader block.Header
	if IAmFullNode {
		latestBlkHeader, _ = FullBlockBytesToBlockLight(latestBlkSer)
	} else {
		latestBlkHeader, _ = HeaderBytesToBlockLight(latestBlkSer)
	}
	
	// perform validity check
	err = SimtaskValidityCheck(simTaskObject, latestBlkHeader)
	if err != nil {
		logger.L.Printf("Received simTask is invalid: %v \nWill ignore this task.\n", err)
		return
	}

	// ---

	// report performance stat that a valid simtask was received (Event SimtaskReceived)
	// 		get current time
	curTimeRightNow := time.Now().UnixNano()
	//		get hash of block problem
	msg := fmt.Sprintf("Unique problem ID: %v", simTaskObject.ProblemHash.GetString())
	//		construct stat to be sent
	pStat := monitoring.NewPerformanceData(curTimeRightNow, MyDockerAlias, monitoring.Event_SimtaskReceived, msg)
	// 		post stat
	err = monitoring.SendPerformanceStatWithRetries(pStat)
	if err != nil {
		logger.L.Printf("Warning - Failed to report performance stat: %v", err)
	}

	// ---

	// update BlockProblemHelper with the newest problem
	BlockProblemHelper.SetSimulationTask(simTaskObject)
	// update BlockProblemHelper to reset miner info from previous block problem
	err = BlockProblemHelper.ResetMiners()
	if err != nil {
		logger.L.Panic(err)
	}
	// update BlockProblemHelper to reset RAcommitSecret
	BlockProblemHelper.SetRAcommitSecret("")
	logger.L.Printf("Successfully set BPH to new problem with id %v.", simTaskObject.ProblemHash.GetString())

	// before starting to work on the current blockproblem store some info about it so that when you finished you can check whether this is still the currently active problem
	oldCurPrblmID := BlockProblemHelper.GetProblemID().GetString()
	oldCurExpTime := BlockProblemHelper.GetProblemExpirationTime()
	
	// print received block problem
	simTaskObject.PrintSimTask()

	// if you are not a miner, stop here
	if curSyncMode != SyncMode_Continuous_Mine {
		logger.L.Printf("Will not try to solve the problem because I am in sync mode: %v", curSyncMode)
		return
	}
	
	// run simulation
	blockProblemSolutionSer, solHash, err := simTaskObject.RunSimulation()
	if err != nil {
		logger.L.Panic(err) // this is a critical error
	}

	// Check whether the problem was solved in time (current timestamp < expiration timestamp && memoryProblemID == currentProblemID [ensure problem has not been updated already which also would have renewed the timestamp])
	currentProblemID := BlockProblemHelper.GetProblemID().GetString()
	if oldCurPrblmID != currentProblemID {
		logger.L.Printf("I solved the block problem with ID %v but I was too slow: In the meantime I already received a new block problem with ID %v and for this reason I will discard my solution.\n", oldCurPrblmID, currentProblemID)
		return
	}
	curTime := block.GetCurrentTime()
	if curTime > oldCurExpTime + uint64(10) { // only submit solution if at least 10 seconds of problem validity are left, otherwise by the time others handle the message your solution might be invalid already
		logger.L.Printf("I solved the block problem with ID %v but I was too slow: The current time is %v but the problem expired at %v + 10 sec. For this reason I will discard my solution.\n", oldCurPrblmID, curTime, oldCurExpTime)
		return
	}

	// Broadcast your commitment [broadcast hash of hash and also broadcast signature of solHash to "pouw_minerCommitments"]
	//	1. get hash of hash (H(H(solution)))
	hashOfSolHash := hash.NewHash(solHash.GetString())
	//	2. get Sig(solHash)
	solSig, err := PrivateKey.Sign(solHash.Bytes)
	if err != nil {
		logger.L.Panic(err)
	}
	// 3. create commitment struct instance and serialize it
	commitment := simsol.MinerCommitment {
		OriginalSenderNodeID: MyNodeIDString,
		HashCommit: hashOfSolHash, 
		SigCommit: solSig, 
	}
	commitmentSer, err := msgpack.Marshal(&commitment)
	if err != nil {
		logger.L.Panic(err)
	}
	// 4. wrap commitment in ts (transport struct)
	tsCommit := NewTransportStruct(TSData_MinerCommitment, MyNodeIDString, commitmentSer)
	// 5. send committment to topic "pouw_minerCommitments"
	err = TopicSendMessage(ctx, "pouw_minerCommitments", tsCommit)
	for err != nil {
		logger.L.Printf("Failed to send topic message: %v\nWill try again..\n", err)
		RandomShortSleep()
		err = TopicSendMessage(ctx, "pouw_minerCommitments", tsCommit)
	}

	logger.L.Printf("Sent commitment to 'pouw_minerCommitments' topic.\n")

	// ---- Ok, now lets send actual solution directly to RA ----

	// send solution to RA
	//		wrap data in TS
	simSolutionTS := NewTransportStruct(TSData_SimSol, MyNodeIDString, blockProblemSolutionSer)

	//		cast RA publicKey to RA peerID
	raPeerID, err := peer.IDFromPublicKey(RApub)
	if err != nil {
		logger.L.Panic(err)
	}

	// 		send data directly to RA
	chatRetryCounter := 0
	err = SendViaChat(simSolutionTS, raPeerID, h, ctx)
	for ((err != nil) && (chatRetryCounter < directChatRetryAmountUpperCap)) {
		logger.L.Printf("TopicNewProblemReceiveEvent - Failed to send my serialized solution to the RA via direct chat: %v\nWill retry sending it via direct chat..", err)
		
		// try again
		RandomShortSleep()
		chatRetryCounter += 1
		err = SendViaChat(simSolutionTS, raPeerID, h, ctx)
	}
	// either it worked (no error) or no more retries are allowed
	if err != nil {
		logger.L.Panicf("Even after retrying %v times I was not able to send my solution to the RA via direct chat. This makes me want to panic!!!", directChatRetryAmountUpperCap)
	}
	logger.L.Printf("Sent my serialized solution with hash %v to the RA.", solHash.GetString())
	
}

// TopicChaindbReceiveEvent is a topic-specific function that is triggered when a node receives a message for topic "pouw_chaindb".
func TopicChaindbReceiveEvent(m pubsub.Message, h host.Host, ctx context.Context) {
	// while in initial SyncMode or in passive SyncMode you ignore these topic messages
	// in other words: you only not ignore these requests, if you are in any continuous mode
	curSyncMode := SyncHelper.NodeModeGet()
	if !(  (curSyncMode == SyncMode_Continuous_Full) || (curSyncMode == SyncMode_Continuous_Light) || (curSyncMode == SyncMode_Continuous_Mine)  ) {
		logger.L.Printf("I am not in a continuous mode, that's why I will ignore this chaindb topic request.")
		return // ignore incoming chaindb topic receive event
	}

	
	// if you are a light node you can only reply if header was requested
	// check whether data could be found (if specific blockhash)

	// 1. ts extraction
	chaindbReqSer, _, err := TransportStructExtraction(TSData_ChainDBRequest, m.Data, false)
	// 2. unpack chaindbreq
	chaindbRequest, err := BytesToChainDBRequest(chaindbReqSer)
	if err != nil {
		logger.L.Printf("Failed to unmarshal received chaindb request received from node %v (which might or might not be the original sender). Ignoring the message.", m.ReceivedFrom)
	}
	// 3. if you are a light node, check whether you can even serve this request (light node can not respond to full block requests)
	if !IAmFullNode {
		if chaindbRequest.WantFullBlock {
			logger.L.Printf("Ignoring chaindb request because it requests a full block but I am a light node that only stores headers.")
			return
		}
	}
	// 4. Ok we try to retrieve the requested data if it exists and respond
	logger.L.Printf("Ok. I will send you the requested data because I am in syncMode %v", curSyncMode)
	var responseDataTSser []byte

	// 5. Depending on which data is requested try to retrieve it

	//		case 1: All block hashes are requested
	if chaindbRequest.WantOnlyAllBlockHashes {
		reqData := GetAllBlockHashesAscendingSerialized(IAmFullNode)
		responseDataTSser = NewTransportStruct(TSData_StringSlice, MyNodeIDString, reqData)
	} else if chaindbRequest.WantOnlyLatestBlock { // 		case 2: 'Latest' block or header is requested
		// case 2.1 full block is requested
		//		retrieve data
		reqData, err := BlockGetBytesFromDb("latest")
		if err != nil {
			logger.L.Panic(err)
		}

		if chaindbRequest.WantFullBlock {
			responseDataTSser = NewTransportStruct(TSData_Block, MyNodeIDString, reqData)
		} else { // case 2.2 only header is requested
			// case 2.2.1 IAmFullNode
			if IAmFullNode {
				// extract header from block
				reqHeaderObject, _ := FullBlockBytesToBlockLight(reqData)
				// serialize header
				reqHeaderSer := HeaderToBytes(reqHeaderObject)
				// wrap serialized header in TS struct
				responseDataTSser = NewTransportStruct(TSData_Header, MyNodeIDString, reqHeaderSer)
			} else { // case 2.2.2 !IAmFullNode (retrieved data is already in required format)
				responseDataTSser = NewTransportStruct(TSData_Header, MyNodeIDString, reqData)
			}
		}
	} else { // case 3: a specific blockHash is requested
		// first check even requested block can even exist
		if len(chaindbRequest.BlockHashStringOfInterest) != 64 {
			logger.L.Printf("Original sender node %v requested a block with an invalid hash (length is not 64 hex chars): %v. Ignoring this request.\n", chaindbRequest.OriginalSenderNodeID, chaindbRequest.BlockHashStringOfInterest)
			return
		}
		// try to retrieve data
		reqData, err := BlockGetBytesFromDb(chaindbRequest.BlockHashStringOfInterest)
		if err != nil {
			logger.L.Printf("Original sender node %v requested blockhash %v but I was not able to retrieve it. It probably does not exist. Ignoring this request.", chaindbRequest.OriginalSenderNodeID, chaindbRequest.BlockHashStringOfInterest)
			return
		}
		// case 3.1 full block is requested
		if chaindbRequest.WantFullBlock {
			responseDataTSser = NewTransportStruct(TSData_Block, MyNodeIDString, reqData)
		} else { // case 3.2 only header is requested
			// case 3.2.1 IAmFullNode
			if IAmFullNode {
				reqHeaderObject, _ := FullBlockBytesToBlockLight(reqData)
				reqHeaderSer := HeaderToBytes(reqHeaderObject)
				responseDataTSser = NewTransportStruct(TSData_Header, MyNodeIDString, reqHeaderSer)
			} else { // case 3.2.2 !IAmFullNode
				responseDataTSser = NewTransportStruct(TSData_Header, MyNodeIDString, reqData)
			}
		}
	}

	// ---- Data has been retrieved, now send it ----

	// 	determine original requester node (NOT not the node who forwarded the message to you)
	//				convert original sender node string back to peer.ID
	originalPeerID, err := peer.Decode(chaindbRequest.OriginalSenderNodeID)
	if err != nil {
		logger.L.Printf("Failed to get peer.ID from OriginalSenderNodeID string %v due to error: %v. Will not be able to respond.\n", chaindbRequest.OriginalSenderNodeID, err)
		return
	}
	if originalPeerID.String() != chaindbRequest.OriginalSenderNodeID {
		logger.L.Printf("OriginalSenderNodeID string %v but the peer.ID I reconstructed in string form is %v and these two are not identical so something must have gone wrong. Will not be able to respond.\n", chaindbRequest.OriginalSenderNodeID, originalPeerID.String())
		return
	}

	// try to connect to it now
	chatStream, err := h.NewStream(ctx, originalPeerID, "/chat/1.0")	// https://github.com/libp2p/go-libp2p/blob/master/core/host/host.go#L65, returns network.Stream
    if err != nil {
        logger.L.Printf("ERROR: %v", fmt.Errorf("TopicChaindbReceiveEvent - %v", err))
        return
    }
	defer chatStream.Close()

	// 		send it
	_, err = chatStream.Write(responseDataTSser)
	if err != nil {
		logger.L.Printf("ERROR: %v", fmt.Errorf("TopicChaindbReceiveEvent - %v", err))
	}
	
	logger.L.Printf("Successfully sent the requested chaindb data to original requester node: %v", chaindbRequest.OriginalSenderNodeID)

}

// TopicNewCommitmentReceiveEvent is a topic-specific function that is triggered when a node receives a message for topic "pouw_minerCommitments".
func TopicNewCommitmentReceiveEvent(m pubsub.Message, h host.Host, ctx context.Context) {

	// you only ignore this data if you are in initial sync
	curSyncMode := SyncHelper.NodeModeGet()
	if (curSyncMode == SyncMode_Initial_Light) || (curSyncMode == SyncMode_Initial_Full) || (curSyncMode == SyncMode_Initial_Mine) {
		logger.L.Printf("I am in an initial sync mode, that's why I will ignore this miner commitment data.")
		return
	}

	// determine nodeID of sender (might or might not be the ORIGINAL sender)
	senderNodeID := m.ReceivedFrom.String()

	// check whether data is valid and can be deserialized into a MinerCommitment
	recDataSer, _, err := TransportStructExtraction(TSData_MinerCommitment, m.Data, false) // dont care about hash
	if err != nil { // signature was probably faked
		logger.L.Printf("Data from node %v (which might or might not be the original sender) was declined for the following reason: %v \n", senderNodeID, err)
		return
	}

	// now that we know it is safe to deserialize the data and it contains what we were expecting , so actually deserialize now
	var recCommitment simsol.MinerCommitment
	err = msgpack.Unmarshal(recDataSer, &recCommitment)
	if err != nil {
		logger.L.Panic(err)
	}
	if recCommitment.OriginalSenderNodeID == "" {
		logger.L.Panicf("Deserialized MinerCommitment seems to have empty OriginalSenderNodeID field! BTW, I received this message from %v. Aborting.", senderNodeID)
	}

	// remember each miners commitment until the next block has been verified and accepted
	newMiner := NewActiveMiner(recCommitment)
	err = BlockProblemHelper.AddMiner(newMiner, false) // dont skip time check as you just now received the commitment in real-time
	if err != nil {
		logger.L.Printf("TopicNewCommitmentReceiveEvent - %v", err)
		return // stop handling the invalid or duplicate commitment that was received
	}

	// ---

	// report performance stat that a new miner commitment was received (Event MinerCommitReceived)
	// 		get current time
	curTimeRightNow := time.Now().UnixNano()
	//		get hash of hash of solution data (kinda zero knowledge)
	msg := fmt.Sprintf("Node %v committed to H(H(sol)) (first 7 chars): %v", recCommitment.OriginalSenderNodeID, recCommitment.HashCommit.GetString()[:7])
	//		construct stat to be sent
	pStat := monitoring.NewPerformanceData(curTimeRightNow, MyDockerAlias, monitoring.Event_MinerCommitReceived, msg)
	// 		post stat
	err = monitoring.SendPerformanceStatWithRetries(pStat)
	if err != nil {
		logger.L.Printf("Warning - Failed to report performance stat: %v", err)
	}

	// ---

	// logger.L.Printf("Received miner commitment from node %v and added it to BPH.", recCommitment.OriginalSenderNodeID)

}

// TopicNewTransactionReceiveEvent is a topic-specific function that is triggered when a node receives a message for topic "pouw_transactions".
func TopicNewTransactionReceiveEvent(m pubsub.Message, h host.Host, ctx context.Context) {
// you only ignore this data if you are in initial sync
	curSyncMode := SyncHelper.NodeModeGet()
	if (curSyncMode == SyncMode_Initial_Light) || (curSyncMode == SyncMode_Initial_Full) || (curSyncMode == SyncMode_Initial_Mine) {
		logger.L.Printf("I am in an initial sync mode, that's why I will ignore this transaction.")
		return
	}

	// determine nodeID of sender (might or might not be the ORIGINAL sender)
	senderNodeID := m.ReceivedFrom.String()

	// light nodes should report that they received a transaction (performance stat) but they will not try to extract it because the validity check would fail because they do not have access to the state which means they can not know whether the tx is valid or not
	if curSyncMode == SyncMode_Continuous_Light {
		// report performance stat

		// 		get current time (ofc it will appear that light nodes receive transactions faster because they do not have to perform validity checks because they report the current time)
		curTimeRightNow := time.Now().UnixNano()
		//		construct stat to be sent
		pStat := monitoring.NewPerformanceData(curTimeRightNow, MyDockerAlias, monitoring.Event_TransactionReceived, "Light nodes do not access transaction details")
		// 		post stat
		err := monitoring.SendPerformanceStatWithRetries(pStat)
		if err != nil {
			logger.L.Printf("Warning - Failed to report performance stat: %v", err)
		}

		return // do not handle this transaction further, you are just a light node
	}

	// check whether transaction is valid (also includes check whether sender owns enough tokens) and can be deserialized into a transaction.Transaction
	recDataSer, _, err := TransportStructExtraction(TSData_Transaction, m.Data, false)
	if err != nil { // e.g. signature was invalid
		logger.L.Printf("Transaction from node %v (which might or might not be the original sender) was declined for the following reason: %v \n", senderNodeID, err)
		return
	}

	// now that we know it is safe to deserialize the transaction and that is actually is a valid transaction, deserialize it
	var recTransaction transaction.Transaction
	err = msgpack.Unmarshal(recDataSer, &recTransaction)
	if err != nil {
		logger.L.Panic(err)
	}

	// ---

	// report performance stat that a new transaction was received (Event TransactionReceived)
	// 		get current time
	curTimeRightNow := time.Now().UnixNano()
	//		get hash of transaction
	msg := fmt.Sprintf("TxHash: %v", recTransaction.TxHash.GetString())
	//		construct stat to be sent
	pStat := monitoring.NewPerformanceData(curTimeRightNow, MyDockerAlias, monitoring.Event_TransactionReceived, msg)
	// 		post stat
	err = monitoring.SendPerformanceStatWithRetries(pStat)
	if err != nil {
		logger.L.Printf("Warning - Failed to report performance stat: %v", err)
	}

	// ---

	// now add it to pending transactions (no duplicates allowed)
	AddPendingTransaction(recTransaction)

}
