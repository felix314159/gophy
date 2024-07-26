package database

// ralogic.go contains Root Authority logic such as defining new block problems, determining accepted solution hash, choosing block winner, creating new blocks, etc.

import (
	"context"
	"crypto/rand" // RAcommit
	"encoding/hex"
	"fmt"
	"time"

	"github.com/libp2p/go-libp2p/core/crypto"

	"example.org/gophy/block"
	"example.org/gophy/block/hash"
	"example.org/gophy/block/simpar"
	"example.org/gophy/block/simsol"
	"example.org/gophy/block/transaction"
	"example.org/gophy/block/winner"
	"example.org/gophy/logger"
	"example.org/gophy/monitoring"
)

// RACreateNewBlock is used by RA to create new chaindb block after block problem has been solved and winner has been chosen.
func RACreateNewBlock(blkWinner winner.BlockWinner, transactionList []transaction.Transaction, simTask simpar.SimulationTask) block.Block {

	// ---- 1. Retrieve prev block ----

	// retrieve previous block from chaindb
	prevBlkSer, err := BlockGetBytesFromDb("latest")
	if err != nil {
		logger.L.Panic(err)
	}
	// deserialize (RA is full node)
	prevBlock := FullBlockBytesToBlock(prevBlkSer)

	// ---- 2. Determine values for new block ----
	blockID := prevBlock.BlockHeader.BlockID + 1					// BlockID
	blockTime := uint64(time.Now().Unix())							// BlockTime
	prevBlockHash := HeaderToHash(prevBlock.BlockHeader)			// PrevBlockHash
	stateMerkleRootHash := hash.NewHash("nil") 						// StateMerkleRoot - put bogus value that will be updated with call to StateDbProcessAndUpdateBlock later
	problemID := simTask.ProblemHash 								// ProblemID
	
	// ---- 3. Create new block ----
	newBlock := block.NewBlock(blockID, blockTime, prevBlockHash, stateMerkleRootHash, problemID, blkWinner, transactionList, simTask)

	//  have RA already affect its statedb to get value statemerkleroothash
	updatedNewBlock, err := StateDbProcessAndUpdateBlock(newBlock)
	if err != nil {
		logger.L.Panic(err)
	}

	return updatedNewBlock

}

// RaGetProblemDefMessage is used to generate a new valid simtask.
// This function should only be used when there is no queued simtask available in the queue, actual problems are always to be prefered.
// It returns:
//		1. signed TransportStruct in msgpacked form so that can be directly sent to topic "pouw_newProblem"
//		2. RAcommit hash
//		3. secretCommit string
func RaGetProblemDefMessage() ([]byte, hash.Hash, string) {
	creationTime := block.GetCurrentTime()
	expirationTime := creationTime + BlockTime // blocktime defines how long each problem is valid (RA is expected to set this to around one day)
	
	// create example problem (simpar) which is valid 10 min (600 sec) and will be solution for block 6
	
	// 		get blockID of latest block and its hash
	blkHashList, err := GetAllBlockHashesAscending(true) // RA is always full node
	if err != nil {
		logger.L.Panic(err)
	}
	latestBlockID := len(blkHashList)-1 // get blockID of latest block
	latestBlockHash := blkHashList[latestBlockID] // access last element of list to get hash of latest block (len-1 means last element)
	seedUint, err := hexStringToUint32(latestBlockHash)	// convert hash to uint32 in a way that is revertible
	if err != nil {
		logger.L.Panic(err)
	}

	// determine RA commit (Keccak256(PrevBlockHash + secretBytes))
	//		generate secret bytes (71 bytes = 568 bits should be enough ;) [2^568 is around 4.6 times the number of legal Go positions (the game)]
	secretBytes := make([]byte, 71) // create a slice with length 71
	_, err = rand.Read(secretBytes)
	if err != nil {
		logger.L.Panic(err)
	}
	secretBytesString := fmt.Sprintf("%x", secretBytes) // convert to hex string of length 2*71 = 142

	//		calculate commit
	raCommit := hash.NewHash(latestBlockHash + secretBytesString)

	// ---- Define subproblems ----

	//		define simulation parameters (adjust here if you want to send different problem for now)
	// 			seed, amountEvents=11, runID=123, particles=0, momentum=2., theta=0. 
	simP1 := simpar.NewSimulationParameters(seedUint, 11, 123, 0, 2., 0.)
	simP2 := simpar.NewSimulationParameters(seedUint, 13, 456, 0, 2.1, 0.)
	//		define header of block problem
	simH := simpar.NewSimulationHeader(creationTime, expirationTime, uint32(latestBlockID+1), uint32(2), raCommit, []simpar.SimulationParameters{simP1, simP2}) // increase blockID by 1 as we define problem to create next block

	// create the block problem
	simTask := simpar.NewSimulationTask([]simpar.SimulationParameters{simP1, simP2}, simH) // for now ignores hash of block problem

	// msgpack the simtask
	simTaskSer := SimtaskToBytes(simTask)

	// TS wrap it
	problemDataReadyToBeSent := NewTransportStruct(TSData_SimulationTask, RANodeID, simTaskSer) // returns signed message that can be sent to topic

	return problemDataReadyToBeSent, raCommit, secretBytesString
}

// RALoop is the loop that the RA is in. It defines new sim tasks, collects results, chooses block winner, creates new block etc.
func RALoop(ctx context.Context) {
	logger.L.Printf("Starting to send sim taks to node network..")
	
	firstIter := true
	for {
		if !(firstIter) {
			// ---- Secret reveal start ----
			// broadcast the until now secret value RAcommitSecret which is needed by everyone to verify winner selection later
			//		retrieve secret
			raCommitSecretString := BlockProblemHelper.GetRAcommitSecret()
			if len(raCommitSecretString) < 7 {
				logger.L.Panicf("RA commit secret string should never be shorter than 7 chars, but retrieved this commit secret: %v", raCommitSecretString)
			}
			//		convert to []byte
			raCommitBytes, err := hex.DecodeString(raCommitSecretString)
			if err != nil {
				logger.L.Panic(err)
			}
			logger.L.Printf("RA will now broadcast commit secret: %v\n", raCommitSecretString)
			tsRaCommit := NewTransportStruct(TSData_RAcommitSecret, MyNodeIDString, raCommitBytes)
			//		broadcast it (try again until the message is sent successfully)
			err = TopicSendMessage(ctx, "pouw_raSecretReveal", tsRaCommit)
			for err != nil {
				logger.L.Printf("Failed to send topic message: %v\nWill try again..\n", err)
				RandomShortSleep()
				err = TopicSendMessage(ctx, "pouw_raSecretReveal", tsRaCommit)
			}

			// ---- Performance stat reporting for RACommitRevealSent ----

			// 		get current time
			curTimeRightNow2 := time.Now().UnixNano()
			//		get msg
			msg := fmt.Sprintf("RA Commit Reveal (first 7 chars): %v", raCommitSecretString[:7]) // only show first 7 chars
			//		construct stat to be sent
			pStat2 := monitoring.NewPerformanceData(curTimeRightNow2, MyDockerAlias, monitoring.Event_RACommitRevealSent, msg)
			// 		post stat
			err = monitoring.SendPerformanceStatWithRetries(pStat2)
			if err != nil {
				logger.L.Printf("Warning - Failed to report performance stat: %v", err)
			}

			// ----

			// ---- Secret reveal end ----

			// before determining eligible winners ensure that every miner that broadcast a valid commitment also has sent us the actual simulation data!
			// if a miner did not send us the actual solution data, the RA will remove that miner from the eligible list and notify the network so that all other nodes do the same (to ensure consensus about who should be eligible winner before winner selection algorithm is run). Note: there sadly is no way for nodes to verify whether RA is lying about having actually received the data or not. But then again: If an honest node would be excluded by the RA news would spread and trust in network lost, so I guess it's not something RA would be interested in lying about
			//toBeExcludedMinersList := GetActiveMinerAddressesThatCommittedButSentNoData()
			// at some point in might be a good idea to implement above fully

			// determine eligible miners that later might be chosen block winner (can't be done at first iteration before any solutions have been collected)
			eligibleActiveMiners := BlockProblemHelper.DetermineEligibleMiners("abc") // put bogus value, ra skips usage of that parameter but go does not allow optional parameters and duplicating the entire function to make a separate one would be even worse
			amountEligibleMiners := len(eligibleActiveMiners)
			
			var winnerNodeAddress string

			winnerWasFound := false
			if amountEligibleMiners > 1 {
				// run non-trivial winner selection algorithm (only 1 miner can be chosen block winner)
				
				winnerNodeAddress = WinnerSelection(eligibleActiveMiners, raCommitSecretString)
				logger.L.Printf("Selected winner node: %v\n", winnerNodeAddress)
				winnerWasFound = true

			} else if amountEligibleMiners == 1 {
				// winner has already been determined
				winnerNodeAddress = eligibleActiveMiners[0].Commitment.OriginalSenderNodeID
				logger.L.Printf("Winner selection was trivial because only one miner is eligible. Winner: %v\n", winnerNodeAddress)
				winnerWasFound = true
			} else {
				logger.L.Printf("No miner is eligible for winner selection. No new block was created, will send out new problem now.\n")
			}

			// permanently store accepted solution in deserialized form on disk
			if winnerWasFound {
				// determine winnner solution hash
				var solutionHashString string
				for _, m := range eligibleActiveMiners {
					if m.Commitment.OriginalSenderNodeID == winnerNodeAddress {
						solutionHashString = m.ActualSolutionHashString
						//logger.L.Printf("Miner address %v has the following actual solution hash stored: %v\n", m.NodeID, m.ActualSolutionHashString)
					}
				}

				if solutionHashString == "" {
					logger.L.Panicf("Warning: The solution hash of the selected winner node (%v) commitment seems to be an empty string. Something went wrong, stopping execution!", winnerNodeAddress)
				}

				// retrieve accepted solution from simdb bucket
				serializedWinnerSolution, err := ReadFromBucket(solutionHashString, "simdb")
				if err != nil {
					logger.L.Panic(err)
				}

				// retrieve blockID that this solution will be bound to
				blkID := BlockProblemHelper.GetBlockID()

				// write winner sim files to filesystem
				simsol.WriteSimsolbytesToFilesystem(serializedWinnerSolution, blkID)

				// create new chaindb block
				//		determine awarded token amount to winner
				tokenReward := winner.GetTokenRewardForBlock(blkID)
				//		create winner instance
				blkWinner := winner.NewBlockWinner(winnerNodeAddress, hash.GetHashObjectWithoutHashing(solutionHashString), tokenReward)
				// 		get other simulation task details
				simTask := BlockProblemHelper.GetSimulationTask()

				//		retrieve pending transactions (fee priority, also respect upper cap per block)
				transactionList, err := RAGetPendingTransactions()
				if err != nil {
					logger.L.Panic(err)
				}
				var validTransactionList []transaction.Transaction
				// non-empty transaction list must be checked for validity so filter out transactions that are not possible, otherwise use zero value which is empty slice
				if len(transactionList) > 0 {
					validTransactionList = TransactionListFilterOutInvalid(transactionList)
				}
				
				//		get new block (THIS ALSO ALREADY AFFECTS THE STATEDB BY PERFORMING THE TRANSACTIONS)
				newBlock := RACreateNewBlock(blkWinner, validTransactionList, simTask)
				
				// Note: RA must now remove the transactions that it included in the block from the list of pending transactions
				PendingTransactionsRemoveThese(validTransactionList)

				//		serialize new block
				newBlockSer := FullBlockToFullBlockBytes(newBlock)
				//		write new block to chaindb
				err = BlockWriteToDb(newBlockSer, true)
				if err != nil {
					logger.L.Panic(err)
				}
				//		print new block
				PrintBlock(newBlock)
				//		get hash of new block
				newBlockHash := HeaderToHash(newBlock.BlockHeader).GetString()
				//		broadcast new block (add some delay so that nodes have time to determine block winner on their own before receiving it)
				// 				wrap in TS struct
				newBlkReadyToBeSent := NewTransportStruct(TSData_Block, MyNodeIDString, newBlockSer)
				//  			wait a bit
				time.Sleep(20 * time.Second)
				//				send it to topic "pouw_newBlock"
				err = TopicSendMessage(ctx, "pouw_newBlock", newBlkReadyToBeSent)
				for err != nil {
					logger.L.Printf("Failed to send topic message: %v\nWill try again..\n", err)
					RandomShortSleep()
					err = TopicSendMessage(ctx, "pouw_newBlock", newBlkReadyToBeSent)
				}

				// ---- Performance stat reporting for RANewBlockSent ----
				
				// 		get current time
				curTimeRightNow2 := time.Now().UnixNano()
				//		get msg
				msg := fmt.Sprintf("Block Hash: %v", newBlockHash)
				//		construct stat to be sent
				pStat2 := monitoring.NewPerformanceData(curTimeRightNow2, MyDockerAlias, monitoring.Event_RANewBlockSent, msg)
				// 		post stat
				err = monitoring.SendPerformanceStatWithRetries(pStat2)
				if err != nil {
					logger.L.Printf("Warning - Failed to report performance stat: %v", err)
				}

				// ----

			}
		}

		// ---- RESET start ----

		// first reset all BPH info (so that while a new problem is being defined and another node would request the current problem it avoids replying with outdated info)
		// 		reset which miners already submitted solution to empty slice
		err := BlockProblemHelper.ResetMiners()
		if err != nil {
			logger.L.Panic(err)
		}
		// 		reset RAcommitSecret
		BlockProblemHelper.SetRAcommitSecret("")
		// 		reset SimulationTask (this is the only time a simtask without set unique hash field is temporarily set)
		BlockProblemHelper.SetSimulationTask(simpar.SimulationTask{})

		// reset simdb bucket (delete all content but not the bucket itself) [must be reset so that RA does not think miner has already uploaded a solution for current block problem]
		err = ResetBucket("simdb")
		if err != nil {
			logger.L.Panic(err)
		} 
		logger.L.Printf("RA - Successfully reset simdb bucket to prepare for next problem.")

		// ---- RESET end ----

		// ---- GENERATE NEW SIMULATION TASK ----

		// get data to send (if there is a queued problem available use it, otherwise generate a random valid simtask)
		var exampleProblem []byte
		var raSecret string
		//		1. try to get problem RA defined manually from queue
		queuedSimtaskIsAvailable, retrievedSimparSlice := IsPendingSimparAvailable()
		if queuedSimtaskIsAvailable {
			logger.L.Printf("Queued simtask has been retrieved, will be sent out as next block problem.")
			exampleProblem, _, raSecret = SimparToSimtask(retrievedSimparSlice)
		} else { // else: generate random simtask
			logger.L.Printf("No queued simtask was found, will generate random block problem.")
			exampleProblem, _, raSecret = RaGetProblemDefMessage()
		}

		// update local storage
		BlockProblemHelper.SetRAcommitSecret(raSecret)

		// update current block problem information
		//		unpack TSstruct
		exampleProblemBytes, _, err := TransportStructExtraction(TSData_SimulationTask, exampleProblem, true)
		if err != nil {
			logger.L.Panic(err)
		}
		//		but first deserialize the problem
		simTaskObject := SimtaskBytesToSimTask(exampleProblemBytes)
		//		update info (just to ensure RA has updated info asap, it would set this info later anyways when it receives the message)
		BlockProblemHelper.SetSimulationTask(simTaskObject)
		
		// wait a bit before you send out new problem so that nodes have time to receive and verify the previous block you might have sent out
		time.Sleep(20 * time.Second)

		// send out problem
		err = TopicSendMessage(ctx, "pouw_newProblem", exampleProblem)
		for err != nil {
			logger.L.Printf("Failed to send topic message: %v\nWill try again..\n", err)
			RandomShortSleep()
			err = TopicSendMessage(ctx, "pouw_newProblem", exampleProblem)
		}

		// ---- Performance stat reporting for RANewProblemSent ----

		// 		get current time
		curTimeRightNow2 := time.Now().UnixNano()
		//		get msg
		msg := fmt.Sprintf("Unique problem ID: %v", simTaskObject.ProblemHash.GetString())
		//		construct stat to be sent
		pStat2 := monitoring.NewPerformanceData(curTimeRightNow2, MyDockerAlias, monitoring.Event_RANewProblemSent, msg)
		// 		post stat
		err = monitoring.SendPerformanceStatWithRetries(pStat2)
		if err != nil {
			logger.L.Printf("Warning - Failed to report performance stat: %v", err)
		}

		// ----

		// wait until current problem expires
		timeSignalChannel := make(chan struct{})
		go waitUntilTimestamp(timeSignalChannel, simTaskObject.SimHeader.ExpirationTime) // get signal when currentTime > ExpirationTime 
		<- timeSignalChannel

		// debug print
		curTimeNow := block.GetCurrentTime()
		logger.L.Printf("Block problem just expired: Now it is %v and the problem expired at %v because it was created at %v and the blocktime is %v seconds\n", curTimeNow, simTaskObject.SimHeader.ExpirationTime, simTaskObject.SimHeader.CreationTime, BlockTime)

		firstIter = false
	}
}

// RAReceivedSimSol is used by the RA to determine the validity of a received simulation solution and to store it if it is valid.
func RAReceivedSimSol(tsData []byte, senderPubKey crypto.PubKey, senderNodeID string) {

	// deserialize TS struct that contains TSData_SimSol
	simDataSer, actualSolutionHash, err := TransportStructExtraction(TSData_SimSol, tsData, false)
	if err != nil {
		logger.L.Printf("RAReceivedSimSol - Received simulation data from miner %v with hash %v is invalid possibly due to error: %v\nIgnoring this received data.\n", senderNodeID, actualSolutionHash, err)
		return
	}

	// ensure amount of received data is not larger than 10 GB
	if len(simDataSer) > 10737418240 {
		logger.L.Printf("RAReceivedSimSol - Received simulation data from miner %v with hash %v is invalid because it is larger than 10 GB which is not allowed!", senderNodeID, actualSolutionHash)
		return
	}

	// deserialize received solution (TSData_SimSol stands for the type simsol.BlockProblemSolution)
	receivedSolution := BytesToSimsolBlockProblemSolution(simDataSer)

	logger.L.Printf("RAReceivedSimSol - Successfully received a solution with hash %v\n", actualSolutionHash)

	// ---- Validity checks ----

	// -1. TS Extraction already checked that claimed hash equals actually received hash

	// 0. ensure that the problemID of the received problem is equal to the ID of the currently active problem. . otherwise notify network that this miner submitted maliciously and its solution will be ignored [this can be proven by publizating the SIGNED ts struct message that was received, RA would not be able to fake this data]
	currentProblemID := BlockProblemHelper.GetProblemID().GetString()
	if currentProblemID != receivedSolution.ProblemHash.GetString() {
		// when banning / reputation systems are implemented: you should now inform network about this event with proof of signed msg that was received and info from which node this was received (so that they can verify the sig)
		logger.L.Printf("RAReceivedSimSol - Received solution from miner %v has invalid problem ID %v. The currently active problem has ID %v\n", senderNodeID, receivedSolution.ProblemHash.GetString(), currentProblemID)
		return
	}

	// 1. wait until from the same node there is a commitment available (for the SAME problem) and ensure the commitment is valid for the data that was received. if not, (notify the other nodes of malicious behavior) and this miner solution is ignored
	commitChannel := make(chan bool)
	go waitForCommitment(commitChannel, currentProblemID, senderNodeID)
	successBool := <- commitChannel // wait until signal has been received (true means commitment received successfully etc, false means problem has changed and no commitment was received until then)


	if !(successBool) {
		// when banning / reputation systems are implemented: notify network of malicious behavior of this node (uploading solution that does not fit to the commitment OR uploading a solution but no valid commitment)
		logger.L.Printf("RAReceivedSimSol - Miner %v was not able to send a valid commitment until problem %v expired! Ignoring its submission.\n", senderNodeID, currentProblemID)
		return
	}


	// ---- Commitment validity checks (with respect to the received solution) ----

	// 		commitment is available so get it 
	commit := BlockProblemHelper.GetCommitment(senderNodeID)


	//		now check whether it is the correct commitment for the solution RA has received from that node
	//			1.A) check whether Sig(solutionHash) is valid
	verifiedSigSuccess, err := senderPubKey.Verify(hash.HashStringGetBytes(actualSolutionHash), commit.SigCommit)
	if (err != nil) || (!(verifiedSigSuccess)) {
		// when banning / reputation systems are implemented: notify network of malicious behavior of this node (commitment sig does not fit to received solution)
		logger.L.Printf("RAReceivedSimSol - Miner %v uploaded a solution that is incompatible with the commitment sent because the signature could not be verified! Debug: %v\n", senderNodeID, err)
		return
	}

	//			1.B) check if H(solutionhash) was calculated correctly
	hashOfHashString := hash.NewHash(actualSolutionHash).GetString()
	recSigHashString := commit.HashCommit.GetString()
	if hashOfHashString != recSigHashString {
		// when banning / reputation systems are implemented: notify network of malicious behavior of this node (commitment hash does not fit to received solution)
		logger.L.Printf("RAReceivedSimSol - Miner %v uploaded a solution that is incompatible with the commitment sent because the H(solutionhash) does not fit to the received solution data! Expected %v but got %v\n.", senderNodeID, hashOfHashString, recSigHashString)
		return
	}

	//			1.C) safely add actual solutionHash to to ActiveMiner struct instance (so that you can later retrieve the solution hash of the winner)
	BlockProblemHelper.UpdateActiveMinerWithActualSolution(senderNodeID, actualSolutionHash)

	// debug:
	//logger.L.Printf("Will try to write (to simdb) solution data of length: %v", len(simDataSer))

	// 2. temporarily store solution until winner selection is completed (after which only winner solution persists on disk)
	// 		write serialized solution (like it was received) to simdb bucket as key-value : <solutionHash>-<serData> (will overwrite existing data, np)
	err = WriteToBucket(actualSolutionHash, simDataSer, "simdb")
	if err != nil {
		logger.L.Panic(err) // RA should never fail to write received data to db
	}

	logger.L.Printf("RA - The commitment of miner %v has passed all tests and fits to the solution. Solution has been accepted and was stored in simdb bucket!\n", senderNodeID)

	// ---- Performance stat reporting for RAReceivedSimulationData (only reports data received that passes the validity checks in place [but this does not guarantee that this solution will make the miner eligible for winner selection later]) ----

	// 		get current time
	curTimeRightNow2 := time.Now().UnixNano()
	//		get msg
	msg := fmt.Sprintf("Received solution data from: %v", senderNodeID)
	//		construct stat to be sent
	pStat2 := monitoring.NewPerformanceData(curTimeRightNow2, MyDockerAlias, monitoring.Event_RAReceivedSimulationData, msg)
	// 		post stat
	err = monitoring.SendPerformanceStatWithRetries(pStat2)
	if err != nil {
		logger.L.Printf("Warning - Failed to report performance stat: %v", err)
	}

	// ----

	// note: by sending same valid solution data over and over again miners could spam the RA, try if this becomes a problem adjust the code to prevent unnecessary db writes

}

// waitUntilTimestamp target a target epoch time in seconds and sends a signal when the current time greater than the given time
func waitUntilTimestamp(timeSignalChannel chan struct{}, targetTime uint64) {
	for {
		curTime := block.GetCurrentTime()
		if curTime > targetTime {
			timeSignalChannel <- struct{}{} // more efficient than bool as it saves one byte
		}

		time.Sleep(1 * time.Second)
	}
	
}

/*
// GetActiveMinerAddressesThatCommittedButSentNoData determines whether every miner that committed to a solution also sent the actual solution data to the RA.
// This function returns the list of all nodes that will be excluded from winner selection because they did not upload actual simulation results to RA. The necessity of this function became apparent when testing with high tc netem loss and corruption rates.
// Note: A node that does not upload their solution data can get lucky and be eligible anyways because the keys of the simdb bucket are the solutionHashes and the RA in this bucket does not remember where the solution came from. So if a node is unable to send their solution but another node has the same solution (which should be the usual case) then the RA accepts both miners to be eligible because it has access to the solution that these nodes committed to
func GetActiveMinerAddressesThatCommittedButSentNoData() []string {
	// miners node IDs that will be made ineligible due to not having uploaded their simulation result data to RA
	var willBeMadeIneligibleMiners []string

	// get list of all miners that committed in a valid way
	committedMinersList := BlockProblemHelper.GetMiners()

	// get list of all solution hashes whose data is locally available to the RA
	receivedSolutionHashes := BoltGetDbKeys("simdb")

	// now you need to hash each of these hashes to get what would have been the HashCommit of the activeminer would calculated the corresponding solution
	var hashesOfReceivedSolutionHashes []string
	for _, curH := range receivedSolutionHashes {
		hashesOfReceivedSolutionHashes = append(hashesOfReceivedSolutionHashes, hash.NewHash(curH).GetString())
	}

	// determine whether there are any miners that should be made ineligible for winner selection
	for _, maybeEligibleMiner := range committedMinersList {
		didAlsoUploadData := false
		for _, hashCommit := range hashesOfReceivedSolutionHashes {
			if maybeEligibleMiner.Commitment.HashCommit.GetString() == hashCommit {
				didAlsoUploadData = true
				break // will only break out of the innermost loop
			}
		}

		if !didAlsoUploadData {
			willBeMadeIneligibleMiners = append(willBeMadeIneligibleMiners, maybeEligibleMiner.Commitment.OriginalSenderNodeID)
		}
	}

	return willBeMadeIneligibleMiners
}
*/