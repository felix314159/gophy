package database

// httpapiqueues.go is used as bridge between locally hosted html websites (send-transaction and send-simtask) and the Golang code. It performs validity checks, input cleanup and other related procedures.

import (
	"fmt"
	"sort"
	"strings"

	"crypto/rand"

	"github.com/felix314159/gophy/block"
	"github.com/felix314159/gophy/block/hash"
	"github.com/felix314159/gophy/block/simpar"
	"github.com/felix314159/gophy/block/transaction"
	"github.com/felix314159/gophy/block/winner"
	"github.com/felix314159/gophy/logger"
)

// keep track of pending transactions and simpars
var PendingTransactions = []transaction.Transaction{}
var PendingSimpars = [][]simpar.SimulationParameters{} // each SimTask can consist of many simpar

// mutexes used for concurrency-safe access (defined in constants.go)
//	Simpar:
//		1. HttpapiSimparMutex (write)
// 		2. HttpapiSimparRMutex (read-only)
// Transactions:
//		1. HttpapiTransactionMutex (write)
// 		2. HttpapiTransactionRMutex (read-only)

// ---- Transactions ----

// TransactionIsValid  takes the string values of the fields of the submit-transaction form and aims to ensure that each field is valid.
// Returns true and the transaction.Transaction object that holds these values in the correct type if the subproblem fields have valid values, otherwise returns false.
func TransactionIsValid(toAddress string, value string, reference string, fee string) (bool, transaction.Transaction) {
	// 1. remove all leading and trailing spaces (would not be very user friendly if invisible chars could lead to problems)
	toAddress 	= strings.TrimSpace(toAddress)
	value 		= strings.TrimSpace(value)
	reference 	= strings.TrimSpace(reference)
	fee 		= strings.TrimSpace(fee)

	// 2. perform validity checks
	//		2.1 toAddress string (must have length 52 chars (because that is the length of the libp2p node ID hex strings I use) and start with 12D3Koo)
	if len(toAddress) != 52 {
		logger.L.Printf("TransactionIsValid - Transaction is invalid because toAddress does not have a length of 64 hex chars, its value is %v", toAddress)
		return false, transaction.Transaction{}
	}
	if strings.ToLower(toAddress)[:7] != "12d3koo" { // i think lowercase should be allowed, at some point test if the cast to public key later still works if its all lowercase
		logger.L.Printf("TransactionIsValid - Transaction is invalid because toAddress does not start with 12D3Koo, its value is %v", toAddress)
		return false, transaction.Transaction{}
	}

	// 		2.2 value float64
	valueFloat64, err := isValidFloat64(value)
	if err != nil {
		logger.L.Printf("TransactionIsValid - Failed to convert value to a float64 because it has value %v", value)
		return false, transaction.Transaction{}
	}
	//			ensure given value is larger than minimum allowed transaction amount
	if valueFloat64 < winner.MinTransactionAmount {
		logger.L.Printf("TransactionIsValid - Transaction value field does not allow values smaller than %v but given value is %v", winner.MinTransactionAmount, valueFloat64)
		return false, transaction.Transaction{}
	}

	// 		2.3 trim reference string when needed and warn user if this occurs (anything after MaxReferenceLength many chars is removed)
	if len(reference) > MaxReferenceLength {
		reference = reference[:MaxReferenceLength]
		logger.L.Printf("TransactionIsValid - Reference field has been trimmed to '%v' because it was too long.\n", reference)
	}

	// 		2.4 fee float64
	feeFloat64, err := isValidFloat64(fee)
	if err != nil {
		logger.L.Printf("TransactionIsValid - Failed to convert fee to a float64 because it has value %v", fee)
		return false, transaction.Transaction{}
	}
	//			ensure given fee is larger than minimum allowed transaction amount (also prevents negative fees to reward tokens to yourself)
	if feeFloat64 < winner.MinTransactionAmount {
		logger.L.Printf("TransactionIsValid - Transaction fee field does not allow values smaller than %v but given value is %v", winner.MinTransactionAmount, feeFloat64)
		return false, transaction.Transaction{}
	}

	// ---- Create transaction object ----
	//	first derive values for fields that do not explicitely have to be set by the user
	// 		3.1 determine 'From' (string) -> MyNodeIDString
	// 		3.2 determine 'TxTime' uint64
	txTime := block.GetCurrentTime()
	//		3.3 determine sender Nonce + 1
	//			3.3.1 first retrieve Sender from statedb
	senderWalletSer, err := ReadFromBucket(MyNodeIDString, "statedb")
	if err != nil {
		logger.L.Printf("TransactionIsValid - Transaction object could not be constructed, because retrieving sender node wallet from state failed. It might not exist if you have never been sent or been awarded any currency. In this case you would not be able to send a transaction anways. Transaction will not be sent.")
		return false, transaction.Transaction{}
	}
	//			3.3.2 unmarshal retrieved data into StateDbStruct object
	senderWallet, err := StateDbBytesToStruct(senderWalletSer)
	if err != nil {
		logger.L.Printf("TransactionIsValid - Failed to unmarshal retrieved Sender statedb data. You probably do not have a wallet in the state yet because you never have been awarded tokens. In this case you can't send a transaction anyways.")
		return false, transaction.Transaction{}
	}
	//			3.3.3 access current nonce and increase it by 1
	reqNonce := senderWallet.Nonce + 1

	// now create transaction object
	transactionObj := transaction.NewTransaction(MyNodeIDString, txTime, toAddress, valueFloat64, reference, reqNonce, feeFloat64, PrivateKey)

	return true, transactionObj
}

// AddPendingTransaction adds a transaction received via the transaction topic handler to the slice PendingTransactions in a concurrency-safe way.
// The transction is only added, if it is not already included in the slice (unique txhash).
// Note: A pending transaction is a transaction that is valid at the point in time that it was added to the slice. However, after choosing which transactions to include in a block the RA must re-check the validity of these transactions in the selected order (e.g. nonce might now be wrong or balance of second transaction sender is now too low)
//       This means that even after retrieving the transactions from the pending transactions slice you still need to ensure that the set of transactions you chose is still valid in the selected order.
func AddPendingTransaction(t transaction.Transaction) {
	HttpapiTransactionMutex.Lock()
	defer HttpapiTransactionMutex.Unlock()

	// upper cap of amount of pending transactions should help at least a bit to prevent [as in no transactions would go through at all anymore which should be noticeable] spam attacks that would make sorting the pending transactions slow (RA needs to find the lowest fee transactions so knowing there can not be 10 billion pending transactions here would be nice)
	if len(PendingTransactions) > 10000 {
		logger.L.Printf("Failed to add transaction with hash %v to list of pending transactions because the list is overloaded! It currently stores %v transactions so no new pending transactions will be accepted at this time. Please try again later.", t.TxHash.GetString(), len(PendingTransactions))
		return
	}

	// ensure this transaction has not been added already
	for _, curT := range PendingTransactions {
		if curT.TxHash.GetString() == t.TxHash.GetString() {
			logger.L.Printf("Duplicate transaction with txhash %v was not added to pending transactions because it already is included", t.TxHash.GetString())
			return
		}

		// don't allow nonces that can't be valid (if the first transaction puts nonce x, then following tx of the same node must have higher nonces) [potential issue: you can't guarantee order in which tx are received via pubsub, so this could become problematic if a node spams many tx in a short amount of time]
		if curT.From == t.From {
			if curT.Nonce >= t.Nonce {
			logger.L.Printf("Transaction with txhash %v was not added to pending transactions because it has Nonce:%v but there already is a queued tx from the same node %v with tx hash %v which puts Nonce:%v\n", t.TxHash.GetString(), t.Nonce, curT.From, curT.TxHash.GetString(), curT.Nonce)
			return
			}
		}

	}

	// one more time ensure that 'Reference' field of transaction is not too long
	if len(t.Reference) > MaxReferenceLength {
		trimmedReference := t.Reference[:MaxReferenceLength]
		t.Reference = trimmedReference
	}

	PendingTransactions = append(PendingTransactions, t)
	logger.L.Printf("Added new transaction with txhash %v to list of pending transactions.", t.TxHash.GetString())
}

// RAGetPendingTransactions is used by RA to get up to <TransactionsPerBlockCap> many transactions from the PendingTransactions slice.
// This function is used before a new block is created to fill it with pending transactions.
// The goal of the algorithm is to prefer high Fee transactions, while also avoiding Nonce conflicts (e.g. same node sends three transactions with nonces 1,2 and 3. Now if tx with Nonce 2 has a fee so low that it is not included in the block, tx with Nonce 3 would be invalid and can not be included as well. you also are not able to dynamically update nonces because then replay attacks would be trivial unless more adjustments in other places are made).
// Note: The transaction list returned by this function must still be checked for validity! For instance, it's possible that after transaction 1 a sender node is too poor for transaction 2 even if it would seem like two valid transactions in isolation.
func RAGetPendingTransactions() ([]transaction.Transaction, error) {
	HttpapiTransactionRMutex.Lock()
	defer HttpapiTransactionRMutex.Unlock()

	if len(PendingTransactions) == 0 {
		logger.L.Printf("There are no pending transactions")
		return []transaction.Transaction{}, nil
	}

	// ---- Phase 1: Prepare by sorting pending transactions ----

	// create copy of PendingTransactions slice so that original is not affected by e.g. sorting
	copyPending := make([]transaction.Transaction, len(PendingTransactions))
	copy(copyPending, PendingTransactions)

	// sort it ascending by NodeID (first factor), then ascending by Nonce (secondary factor), then ascending by Fee (third factor)
	sort.Slice(copyPending, func(i, j int) bool {
		if copyPending[i].From != copyPending[j].From {
			return copyPending[i].From < copyPending[j].From
		}
		if copyPending[i].Nonce != copyPending[j].Nonce {
			return copyPending[i].Nonce < copyPending[j].Nonce
		}
		return copyPending[i].Fee > copyPending[j].Fee
	})

	// ---- Phase 2: Start copying transactions, but only as long as both upper caps are respected and each sender's nonce only occurs once ----

	//	use map to track how many tx each sender has included in the current block already
	countMap := make(map[string]int) // From -> Fee
	//  	use counter to track how many tx have been included in this block in total
	txCounter := 0

	var almostFinalTxList []transaction.Transaction
	for _, t := range copyPending {
		// ensure upper cap for tx/block is respected
		if txCounter >= TransactionsPerBlockCap {
			break
		}

		// ensure upper cap for txPerSenderPerBlock is respected
		fromCounter, exists := countMap[t.From]
		if exists {
			if countMap[t.From] >= TransactionsPerNodePerBlockCap {
				continue // this tx can not be included because the Sender node has already reached his cap for this block
			}
		}

		// only add the tx to the new list if it does not already contain a tx from the same sender with the same nonce
		allowed := true
		for _, tTemp := range almostFinalTxList {
			if tTemp.From == t.From {
				if tTemp.Nonce == t.Nonce {
					allowed = false
				}
			}
		}
		if allowed {
			almostFinalTxList = append(almostFinalTxList, t)
			txCounter += 1 // for global tx counter for this block

			if exists {
				countMap[t.From] = fromCounter + 1 	// when added 'From' does exist only increase its counter by 1
			} else {
				countMap[t.From] = 1 				// otherwise initialize it to 1
			}
		}

	}

	// ---- Phase 3: Resolve nonce conflicts ----
	// Rationale: Due to capping amount of tx per sender and sorting by fee it now is possible that a higherNonce-higherFee tx of a sender now comes before a lowerNonce tx of the same Sender. Since this is not possible, invalid transactions are now being removed to guarantee a valid resulting tx list. Sender Nodes are advised to avoid tx finalization delays by giving the highest fee for their first tx and decreasing fees for each additional tx they try to have included in the same block.

	// GOAL: for each tx of sender x, ensure that in the tx list each tx nonce of x's tx's follows with a nonce that is not increased by 1

	var finalTxList []transaction.Transaction
	var prevTx transaction.Transaction
	for index, t := range almostFinalTxList {
		// at last element stop because last+1 does not exist
		if index == len(almostFinalTxList) {
			break
		}

		if prevTx.From != t.From { // if prevTx has not been set this works too because it will an empty string
			// this is the first tx of a sender that did not occur before so the tx is allowed
			prevTx = t
			finalTxList = append(finalTxList, t)
			continue
		}

		// same sender has already another tx included, so let's see if current nonce is valid (increased by 1)
		if prevTx.Nonce + 1 == t.Nonce {
			// it is valid, so include the current tx too
			prevTx = t
			finalTxList = append(finalTxList, t)
		}
	}

	return finalTxList, nil

	// Note1 : This algorithm slightly favors alphabetically-early nodeIDs in rare scenarios where lots of pending transactions lead to situations where not all transactions can be included in the next block. A 'fairer' algorithm here would significantly be more complex because the nonce validity has to be ensured at the same time while also generally preferring high fee transactions.
	// Note2: Even though this algorithm filters out duplicate tx nonces from the same 'From', you should ensure that the function that adds incoming transactions to the PendingTransactions queue does not allow dupliate nonces (or invalidly low nonces), because while filtered out at first execution they would appear the second time but would be invalid by then (this function assumes that the lowest nonce of a sender's queued transactions should be valid).
}

// PendingTransactionsRemoveThese takes a list of transactions that were included in a block and removes them from pending transactions if possible
// Note: AFAIK there should be no need to panic if a transaction that is not in pending was included in a block. RA can not forge transactions due to the signatures and can not replay them due to the Nonces.
func PendingTransactionsRemoveThese(tl []transaction.Transaction) {
	HttpapiTransactionMutex.Lock()
	defer HttpapiTransactionMutex.Unlock()

	newPendingTransactionList := []transaction.Transaction{}

	for _, pT := range PendingTransactions {
		wasIncludedInABlock := false
		pTtxHashString := pT.TxHash.GetString()
		for _, includedT := range tl {
			if includedT.TxHash.GetString() == pTtxHashString {
				wasIncludedInABlock = true
				break // breaks to inside outer loop but not fully outside (this transaction will not be remembered in pending transactions)
			}
		}
		
		// if this pending transaction was not included in a block yet, remember it as pending
		if !wasIncludedInABlock {
			newPendingTransactionList = append(newPendingTransactionList, pT)
		} else {
			logger.L.Printf("Removed transaction %v from PendingTransactions", pTtxHashString)
		}
		
	}

	// permanently remember new pending transaction slice
	PendingTransactions = newPendingTransactionList
}

/*
// PendingTransactionsGetAllSerialized retrieves all pending transactions, serializes them and returns them.
func PendingTransactionsGetAllSerialized() []byte {
	HttpapiTransactionRMutex.Lock()
	defer HttpapiTransactionRMutex.Unlock()

	allPtSer, err := TransactionSliceToBytes(PendingTransactions)
	if err != nil {
		logger.L.Panic(err)
	}

	return allPtSer
}
*/

// GetPendingTransactionSlice returns slice of currently pending transactions.
// Used by RA to reply to live data requests.
func GetPendingTransactionSlice() []transaction.Transaction {
	HttpapiTransactionRMutex.RLock()
	defer HttpapiTransactionRMutex.RUnlock()

	return PendingTransactions

}

// ---- Simtasks ----

// SubproblemIsValid takes the string values of the fields of the submit-simtask form and aims to ensure that each field is valid (e.g. no string in a field where uint32 is expected).
// Returns true and the SimulationParameters object that holds these values in the correct type if the subproblem fields have valid values, otherwise returns false.
func SubproblemIsValid(amountEvents string, runID string, particles string, momentum string, theta string) (bool, simpar.SimulationParameters) {
	// 1. remove all leading and trailing spaces (would not be very user friendly if invisible chars could lead to problems)
	amountEvents = strings.TrimSpace(amountEvents)
	runID = strings.TrimSpace(runID)
	particles = strings.TrimSpace(particles)
	momentum = strings.TrimSpace(momentum)
	theta = strings.TrimSpace(theta)

	// 2. perform validity checks
	// 		2.1 amountEvents uint32
	amountEventsUint32, err := isValidUint32(amountEvents)
	if err != nil {
		logger.L.Printf("SubproblemIsValid - Failed to convert amountEvents to a uint32 because it has value %v", amountEvents)
		return false, simpar.SimulationParameters{}
	}
	//				ensure more than 0 events are given
	if amountEventsUint32 < 1 {
		logger.L.Printf("SubproblemIsValid - amountEvents has value %v which is not allowed. Min value: 1", amountEvents)
		return false, simpar.SimulationParameters{}
	}

	// 		2.2 runID uint64
	runIDUint64, err := isValidUint64(runID)
	if err != nil {
		logger.L.Printf("SubproblemIsValid - Failed to convert runID to a uint64 because it has value %v", runID)
		return false, simpar.SimulationParameters{}
	}

	// 		2.3 particles uint32
	particlesUint32, err := isValidUint32(particles)
	if err != nil {
		logger.L.Printf("SubproblemIsValid - Failed to convert particles to a uint32 because it has value %v", particles)
		return false, simpar.SimulationParameters{}
	}
	//				currently only values 0 (pions), 1 (eplus) and 2 (proton) are supported
	if particlesUint32 != 0 && particlesUint32 != 1 && particlesUint32 != 2 {
		logger.L.Printf("SubproblemIsValid - particles must have values 0 (pions), 1 (eplus) or 2 (proton) but got value %v", particles)
		return false, simpar.SimulationParameters{}
	}

	// 		2.4 momentum float64
	momentumFloat64, err := isValidFloat64(momentum)
	if err != nil {
		logger.L.Printf("SubproblemIsValid - Failed to convert momentum to a float64 because it has value %v", momentum)
		return false, simpar.SimulationParameters{}
	}
	//			ensure given value is larger than 0
	if momentumFloat64 <= 0.0 {
		logger.L.Printf("SubproblemIsValid - momentum does not allow values smaller than zero but given value is %v", momentum)
		return false, simpar.SimulationParameters{}
	}

	// 		2.5 theta float64
	thetaFloat64, err := isValidFloat64(theta)
	if err != nil {
		logger.L.Printf("SubproblemIsValid - Failed to convert theta to a float64 because it has value %v", theta)
		return false, simpar.SimulationParameters{}
	}
	//			ensure given value is non-negative
	if thetaFloat64 < 0.0 {
		logger.L.Printf("SubproblemIsValid - theta does not allow negative values but given value is %v", thetaFloat64)
		return false, simpar.SimulationParameters{}
	}


	// ---- Create simpar object ----
	//	use zero value for seed which later will be determined (later = when the simtask is needed, for now it's just put in the queue)
	simparObj := simpar.NewSimulationParameters(0, amountEventsUint32, runIDUint64, particlesUint32, momentumFloat64, thetaFloat64)

	return true, simparObj
}

// AddPendingSimpar is used after the RA used to locally hosted website to broadcast a new simulation task to add the input values as SimulationParameters object to the slice PendingSimpars.
// Note 1: The 'Seed' fields (uint32) of s have not been set yet because they have to be determined in real-time whenever a new simpar slice is retrieved from the queue (Seed which depends on previous block hash which depends on when the simpar slice is retrieved).
// Note 2: This function assumes that the validity of each simpar in the passed slice has been checked for validity using SubproblemIsValid.
func AddPendingSimpar(simparSlice []simpar.SimulationParameters) {
	HttpapiSimparMutex.Lock()
	defer HttpapiSimparMutex.Unlock()

	if len(simparSlice) == 0 {
		logger.L.Printf("You tried to add an empty simpar slice to the queue, this is not allowed.")
		return
	}

	PendingSimpars = append(PendingSimpars, simparSlice)
	logger.L.Printf("New simpar slice of length %v has been added to queue.", len(simparSlice))
}

// IsPendingSimparAvailable checks whether PendingSimpars is empty or not. Returns true and the first added simpar slice if there is sth in the queue, returns false and zero value of SimulationParameters when empty.
// This function is used by the RA when a new block has been created and a new simtask should be sent out: If a pending simpar slice 
// is available it will be used to construct the next SimulationTask (for this additional parameters will have to be determined), otherwise RA will automatically generate a random simtask (to keep transaction throughput up).
func IsPendingSimparAvailable() (bool, []simpar.SimulationParameters) {
	HttpapiSimparRMutex.RLock()
	defer HttpapiSimparRMutex.RUnlock()

	if len(PendingSimpars) == 0 {
		logger.L.Printf("IsPendingSimtaskAvailable - No pending simpar slice is available.")
		return false, []simpar.SimulationParameters{}
	}

	// get oldest simpar slice
	retrievedSimpar := PendingSimpars[0]

	// remove this element from the slice
	PendingSimpars = PendingSimpars[1:]

	return true, retrievedSimpar
}

// SimparToSimtask takes a simpar slice that was retrieved from the queue 'PendingSimpars', sets the correct value for each 'Seed' field, then determines further values needed to construct SimulationTask and then returns 3 values:
// It returns:
//		1. signed TransportStruct in msgpacked form so that can be directly sent to topic "pouw_newProblem"
//		2. RAcommit hash
//		3. secretCommit string
func SimparToSimtask(simparSlice []simpar.SimulationParameters) ([]byte, hash.Hash, string) {
	if !IAmRA {
		logger.L.Printf("SimparToSimtask - There is no point in trying to perform this as only SimTasks are only created by the RA and even if other nodes were to send out simtasks they would be ignored due to their signature not being from the RA.")
		return []byte{}, hash.Hash{}, ""
	}

	// 1. retrieve 'latest' chaindb blockhash and derive correct 'Seed' field from it, then update Seed of each subproblem
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
	//		1.1 set Seed field of each contained simpar
	for index := range simparSlice {
		simparSlice[index].Seed = seedUint // ensures that you modify the original instead of a copy
	}
	
	// 2. determine secret bytes and RAcommit
	secretBytes := make([]byte, 71)
	_, err = rand.Read(secretBytes)
	if err != nil {
		logger.L.Panic(err)
	}
	secretBytesString := fmt.Sprintf("%x", secretBytes)
	raCommit := hash.NewHash(latestBlockHash + secretBytesString)

	// 3. determine values for SimulationHeader
	creationTime := block.GetCurrentTime()					// 3.1 Problem Creation Time
	expirationTime := creationTime + BlockTime 				// 3.2 Problem Expiration Time
	idOfBlockThatWillBeCreated := uint32(latestBlockID+1)	// 3.3 ID of block that will be created by solving this simulation task
	amountOfSubproblems := uint32(len(simparSlice)) 		// 3.4 Amount of subproblems contained in this simulation task

	// 4. create SimulationHeader
	simH := simpar.NewSimulationHeader(creationTime, expirationTime, idOfBlockThatWillBeCreated, amountOfSubproblems, raCommit, simparSlice)

	// 5. create the simulation task
	simTask := simpar.NewSimulationTask(simparSlice, simH)

	// 6. msgpack the simtask
	simTaskSer := SimtaskToBytes(simTask)

	// 7. get it TS wrapped
	problemDataReadyToBeSent := NewTransportStruct(TSData_SimulationTask, RANodeID, simTaskSer)

	return problemDataReadyToBeSent, raCommit, secretBytesString

}
