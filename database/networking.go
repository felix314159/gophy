package database

// networking.go is used to sync nodes with each other, it contains helper structs such as SyncHelper and BlockProblemHelper.

import (
	"bufio"
	"context"
	"fmt"
	"io"
	unsafeRand "math/rand" // used to get non-cryptographically secure pseudo-random numbers used for sleeping when needed (e.g. networking-related request failed and should be tried again after short delay)
	"sort"
	"strconv"
	"strings"
	"time"

	// libp2p stuff
	"github.com/libp2p/go-libp2p/core/host"
	"github.com/libp2p/go-libp2p/core/network"
	"github.com/libp2p/go-libp2p/core/peer"

	"github.com/vmihailenco/msgpack/v5"

	"github.com/felix314159/gophy/block"
	"github.com/felix314159/gophy/block/hash"
	"github.com/felix314159/gophy/block/simpar"
	"github.com/felix314159/gophy/block/simsol"
	"github.com/felix314159/gophy/block/transaction"
	"github.com/felix314159/gophy/logger"
	"github.com/felix314159/gophy/monitoring"
)

// ---- Sync related ----

/*
Explanations of the different modes:

- Initial: 		Requests all blockhashes, then requests each block one by one, then builds statedb (usually done in Passive mode). Will then change to Continuous mode.
- Continuous: 	Similar to initial but tries to re-use existing blockchain data and only requests missing data. Keeps listening for new blocks while also helping other nodes. A miner is a full node that actively runs simulations as new problems come in.
- Passive:		Stay in sync but don't help other nodes (ignore any requests direct/via topic). This mode is useful while building statedb, so that requests from other nodes do not block database access which could slow down the initial sync if it would happen extremely often.

Generally, if a new node is created Initial mode is used. During initial mode it automatically switches to passive mode for intensive tasks but eventually it will end up in Continuous mode.
If your node has been down for some time you can start it directly with Continuous mode, it will determine and request only the data it missed when being offline.
Simplified, the internal upgrade paths are one of the following:

- Initial_Full -> Continuous_Full (full node that does not work on problems but helps everyone with syncing)
- Initial_Light -> Continuous_Light (light node that not work on problems and helps when it can but it can't help with certain requests right now)
- Initial_Mine -> Continuous_Mine (full node that actively runs simulations and also helps others with syncing)

*/

// Mode is an int alias used to implement what would be called an enum in other languages (which Go does not have yet as of writing)
type Mode int

// String implements the string interface for Mode so that the name of the enum element will be printed instead of its int value.
func (e Mode) String() string {
	switch e {
	case SyncMode_Initial_Full:
		return "SyncMode_Initial_Full"
	case SyncMode_Initial_Light:
		return "SyncMode_Initial_Light"
	case SyncMode_Initial_Mine:
		return "SyncMode_Initial_Mine"
	case SyncMode_Continuous_Full:
		return "SyncMode_Continuous_Full"
	case SyncMode_Continuous_Light:
		return "SyncMode_Continuous_Light"
	case SyncMode_Continuous_Mine:
		return "SyncMode_Continuous_Mine"
	case SyncMode_Passive_Full:
		return "SyncMode_Passive_Full"
	case SyncMode_Passive_Light:
		return "SyncMode_Passive_Light"
	default:
		return fmt.Sprintf("%d", e)
	}
}

// StringToMode takes a string that represents a Mode and returns the Mode and error. Error will only be nil if the given string represents a valid Mode.
func StringToMode(modeString string) (Mode, error) {
	// user should be allowed to enter case insensitive string
	switch strings.ToLower(modeString) {
	case "syncmode_initial_full":
		return SyncMode_Initial_Full, nil
	case "syncmode_initial_light":
		return SyncMode_Initial_Light, nil
	case "syncmode_initial_mine":
		return SyncMode_Initial_Mine, nil
	case "syncmode_continuous_full":
		return SyncMode_Continuous_Full, nil
	case "syncmode_continuous_light":
		return SyncMode_Continuous_Light, nil
	case "syncmode_continuous_mine":
		return SyncMode_Continuous_Mine, nil
	case "syncmode_passive_full":
		return SyncMode_Passive_Full, nil
	case "syncmode_passive_light":
		return SyncMode_Passive_Light, nil
	default:
		return 0, fmt.Errorf("StringToMode - Given string %v can not be mapped to a Node Mode! \n", modeString)
	}
}

// ---- SyncHelperStruct declaration and methods ----

// SyncHelperStruct is a custom struct that a node uses to keep track of its syncmode, which data it has received and from whom and how often etc.
type SyncHelperStruct struct {
	NodeMode			Mode							// Determines behavior of node (affects how incoming data is handled / replied to).
	ConfirmationsReq	int								// Node sets target confirmation amount (only if this many other nodes sent same data it is accepted)
	Data				map[string]struct {				// Keep track of which data was received from other nodes how many times. string is hash of (possibly subset of) Data. [e.g. if block.Block is received, only block.Header is hashed to determine map key]
							ConfirmationsCur	int  	// Node keeps counting how many confirmations for any wanted piece of data is has 
							Data				[]byte
						}
	Senders				[]string						// holds NodeIDs so that each sender can only send a piece of data once (otherwise same sender could boost ConfirmationsCur by sending same data multiple times). Senders does not allow duplicate values.
}

// NodeModeWrite changes the node mode by setting it to a new value. This is a write operation and must be handled with mutex.
func (s *SyncHelperStruct) NodeModeWrite(i Mode) {
	networkingMutex.Lock()
	defer networkingMutex.Unlock()

	s.NodeMode = i
	logger.L.Printf("Switched node mode to %v", i)
}

// NodeModeGet returns the current node mode as int. Uses RLock to allow concurrent reads.
func (s *SyncHelperStruct) NodeModeGet() Mode {
	networkingMutex.RLock()
	defer networkingMutex.RUnlock()

	return s.NodeMode
}

// ConfirmationsReqWrite changes the amount of confirmations required until data is accepted.
// If the correct hash is known already, this function is used to set Req to 1.
func (s *SyncHelperStruct) ConfirmationsReqWrite(i int) {
	networkingMutex.Lock()
	defer networkingMutex.Unlock()

	s.ConfirmationsReq = i
}

// ConfirmationsReqGet returns the amount of data confirmations that are needed until the data is accepted.
// Higher value provides potentially more security but is slower and can lead to sync not working if not enough nodes are online.
func (s *SyncHelperStruct) ConfirmationsReqGet() int {
	networkingMutex.RLock()
	defer networkingMutex.RUnlock()

	return s.ConfirmationsReq
}

// MapAdd adds a new element to the map. Data is received from other nodes, then it is hashed to get the mapKey. 
// Every time the same data is received the int counter is increased by one. This way, the map can keep track of which data
// has been received how many times. This enables a fundation for consensus between nodes.
// Note:	This functions automatically determines whether the key already exists and its occurances counter should be increased by one,
//			or whether the key is new and the occurances should be set to 1. It also is able to initialize an empty map should it currently be nil.
func (s *SyncHelperStruct) MapAdd(data []byte, mapKey string, senderNodeIDstring string) {
	// ignore this data if that nodeID has already sent data that was accepted
	if !(s.SendersIsNew(senderNodeIDstring)) {
		return
	} else {	// add new sender to Senders
		s.SendersAddUnique(senderNodeIDstring)
	}
	
	networkingMutex.Lock()
	defer networkingMutex.Unlock()

	// if the map is nil, initialize an empty map
	if s.Data == nil {
		s.Data = make(map[string]struct {
			ConfirmationsCur	int
			Data				[]byte
		})
	}

	val, existsAlready := s.Data[mapKey]
	// if key already existed, increase int by 1
	if existsAlready {
		// debug
		if DebugLogging {
			logger.L.Printf("MapAdd - Received data confirmation (hash was already seen before) from node %v and will increase counter of SyncHelper.Data[%v]\n", senderNodeIDstring, mapKey)
		}
		
		val.ConfirmationsCur++      // modify a copy of the map element
		s.Data[mapKey] = val // now assign the copy to the ACTUAL element (without this line counter is not permanently increased)
	} else { // otherwise set int to 1
		// debug
		if DebugLogging {
			logger.L.Printf("MapAdd - Received new data (hash not seen before) from node %v and will add it to SyncHelper.Data[%v]\n", senderNodeIDstring, mapKey)
			logger.L.Printf("Added hash of received blockhash StringSlice to SyncHelper map.")
		}

		s.Data[mapKey] = struct {
			ConfirmationsCur	int
			Data				[]byte
		}{ConfirmationsCur: 1, Data: data}

	}

}

// MapCheckIfConfirmationsReached checks if any piece of data has been received often enough to be accepted, then returns true and the accepted data.
// Otherwise returns false and nil.
func (s *SyncHelperStruct) MapCheckIfConfirmationsReached() (bool, []byte, string) {
	networkingMutex.RLock()
	defer networkingMutex.RUnlock()

    for hashString, mapElement := range s.Data {
        if mapElement.ConfirmationsCur >= s.ConfirmationsReq {
			// reset Senders (so that same nodes are allowed to send data you will request next)
			s.Senders = []string{}
			
            return true, mapElement.Data, hashString
        }
    }
    return false, nil, ""
}

// MapReset resets the Data map to empty. This is done after you successfully got data you wanted.
func (s *SyncHelperStruct) MapReset() {
	networkingMutex.Lock()
	defer networkingMutex.Unlock()

	// reset map to being empty (not nil!)
    s.Data = make(map[string]struct {
        ConfirmationsCur int
        Data             []byte
    })

    logger.L.Printf("SyncHelper Map has been reset")
}

// SendersAddUnique takes nodeID and adds it to Senders (but only if it was not in there already)
func (s *SyncHelperStruct) SendersAddUnique(newSender string) {
	// do nothing if sender already was in Senders slice
	if !(s.SendersIsNew(newSender)) {
		return
	}
	
	networkingMutex.Lock()
	defer networkingMutex.Unlock()

	// add new sender to the slice
	s.Senders = append(s.Senders, newSender)

	//logger.L.Printf("Added new sender %v to SyncHelper slice", newSender)
}

// SendersIsNew returns bool value that describes whether a given sender is new (true - not in Senders) or known (false - sender already was in Senders). Uses rmutex to allow concurrent reads.
func (s *SyncHelperStruct) SendersIsNew(newSender string) bool {
	networkingMutex.RLock()
	defer networkingMutex.RUnlock()

	for _, existingSender := range s.Senders {
		if existingSender == newSender {
			return false
		}
	}

	return true

}

// SyncNode starts an automatic sync between nodes.
// 		Phase 1: First initial sync mode is triggered where a list of blockHashes is requested from multiple nodes until it is accepted.
// 		Phase 2: Then each block is requested indiviudally
// 		Phase 3: When all blocks are received the blockchain is checked for validity (builds statedb too).
// Then you switch to the final sync mode (there are flowcharts that define which initial mode ends up in which final mode):
//   	Phase 4: Then the nodes switches to its final sync mode which affects how it will behave.
func SyncNode(ctx context.Context, h host.Host) {
	// ---- Phase 1: Initial Sync: Get blockHeaders ----

	// get initial ConfirmationsReq that was passed
	initialConfReq := SyncHelper.ConfirmationsReqGet()

	// get all blockHashes
	//		1. get ChainDBRequest instance
	bHReq := NewChainDBRequest(MyNodeIDString, true, false, false, "")
	//		2. serialize it
	bHReqSer, err := ChainDBRequestToBytes(bHReq)
	if err != nil {
		logger.L.Panic(err)
	}
	// 		3. wrap in TS
	bHReqTS := NewTransportStruct(TSData_ChainDBRequest, MyNodeIDString, bHReqSer)

	// 4. repeatedly keep requesting blockHeaders in regular intervals until enough responses have been collected
	acceptedDataChannel := make(chan []byte)
    go waitForData(acceptedDataChannel)

    // 		re-request interval: 10 sec
    reRequestDataTicker := time.NewTicker(10 * time.Second)

    acceptedData := func() []byte {
        for {
            select {
            case <-reRequestDataTicker.C: // ticker.C is used to perform an action in regular intervals (here 10 seconds), action is send request again
                // okay but keep attempting until sending message is successful (so you want to send 1 message every 10 sec, but if that fails you just keep retrying until you sent that 1 message)
                err := TopicSendMessage(ctx, "pouw_chaindb", bHReqTS)
                for err != nil {
					logger.L.Printf("Failed to send topic message: %v\nWill try again..\n", err)
					RandomShortSleep()
					err = TopicSendMessage(ctx, "pouw_chaindb", bHReqTS)
				}

        	case acceptedData := <-acceptedDataChannel: // case: this is not a ticker event (every 10 sec you would not get the default case)
            		logger.L.Printf("Verified blockheaders have been received.")
            		return acceptedData
        	}
        }
    }()

	// ----

	// now that the list of all blockHashes is known, unmarshal the received data into []string
	acceptedDataStringSlice := BytesToStringSlice(acceptedData)

	// ensure that there are no duplicates in the string slice
	acceptedDataStringSlice = RemoveDuplicateStrings(acceptedDataStringSlice)

	// now check if any data locally is missing [panic means either local node is corrupted/flawed or current majority of nodes we communicated with is malicious]
	needTheseBlockHashesStringSlice, err := DetermineLocallyMissingBlockHashes(acceptedDataStringSlice, IAmFullNode)
	if err != nil {
		logger.L.Panicf("SyncNode - Accepted data has been compared with local state but failed: %v\nPlease re-sync your node.", err)
	}
	
	// ---- Phase 2: Initial Sync: Get blocks/headers ----
	
	if IAmFullNode {
		logger.L.Printf("Will now request full blocks.")
	} else {
		logger.L.Printf("Will now request headers.")
	}

	// set ConfirmationsReq to 1 (no matter what they were before) because now the correct hashes are known
	SyncHelper.ConfirmationsReqWrite(1)

	// request each block / header one-by-one (ascending by ID) [includes genesis block]
	//		technically like above you could also repeat this action in a loop until you actually received all data, but I found so far that's not necessary, when someone replied to you above it means you should be well-connected to the network which means you should get responses, so far no need for request loop
	for _, blockHashString := range needTheseBlockHashesStringSlice {
		
		// request block or header
		//		1. create ChainDBRequest instance
		chaindbReq := NewChainDBRequest(MyNodeIDString, false, IAmFullNode, false, blockHashString)
		//		2. serialize it
		chaindbReqSer, err := ChainDBRequestToBytes(chaindbReq)
		if err != nil {
			logger.L.Panic(err)
		}
		// 		3. wrap in TS
		haindbReqTS := NewTransportStruct(TSData_ChainDBRequest, MyNodeIDString, chaindbReqSer)
		
		// ---- Request data and wait until it has been received once (ConfirmationsReq was set to 1), hash of good data is already known here ----

		// reset channel related data types
		acceptedData = []byte{}
		acceptedDataChannel = make(chan []byte)

		// wait until data of interest has been received
	    go waitForDataWithHash(blockHashString, acceptedDataChannel)

	    // 		re-request interval: 10 sec (if after 10 sec the data of interest has not been received, request it again)
	    reRequestBlockDataTicker := time.NewTicker(10 * time.Second)

	    recData := func() []byte {
	        for {
	            select {
	            case <-reRequestBlockDataTicker.C:
	                // send request for this piece of block data, repeat if it fails
	                err := TopicSendMessage(ctx, "pouw_chaindb", haindbReqTS)
	                for err != nil {
						logger.L.Printf("Failed to send topic message: %v\nWill try again..\n", err)
						RandomShortSleep()
						err = TopicSendMessage(ctx, "pouw_chaindb", haindbReqTS)
					}
				// as soon as data with correct hash has been received stop requesting this piece of data and move on to the next block / header of interest
	            
		        case acceptedData = <-acceptedDataChannel:
		        	logger.L.Printf("Received data for block with hash %v.", blockHashString)
		        	return acceptedData
	        	}
	        }
	    }()

		// write data of this block to chaindb
		err = BlockWriteToDb(recData, IAmFullNode)
		if err != nil {
			logger.L.Panicf("Failed to write block data to database: %v", err)
		}
		logger.L.Printf("New block data has been written to database.")
		
	}

	// ---- Ok so all block data has been received now ----

	if len(needTheseBlockHashesStringSlice) == 0 { // in this case the entire block above would have been skipped anyways, it's just to inform the continuous sync user that it was already up-to-date
		logger.L.Printf("There was no need to request any block data because I already was up-to-date.")
	}

	// remember that you received all chaindb data so that you are from now on able to handle incoming blocks via the topic
	initialSyncChaindbDataWasReceivedAlready = true

	// change nodemode to Passive (declines incoming connections) [avoids db locks while intensive task is going on]
	if IAmFullNode {
		SyncHelper.NodeModeWrite(SyncMode_Passive_Full)
	} else {
		SyncHelper.NodeModeWrite(SyncMode_Passive_Light)
	}
	logger.L.Printf("Sync success.\nChecking validity of received data..\n")
	
	// ---- Performance stat reporting for  Event_InitialSyncChaindbReceived ----

	// report performance stat
	// 		get current time
	curTimeRightNow := time.Now().UnixNano()
	//		construct stat to be sent
	pStatReceived := monitoring.NewPerformanceData(curTimeRightNow, MyDockerAlias, monitoring.Event_InitialSyncChaindbReceived, "foo") // no field can be empty so put bogus message
	// 		post stat
	err = monitoring.SendPerformanceStatWithRetries(pStatReceived)
	if err != nil {
		logger.L.Printf("Warning - Failed to report performance stat: %v", err)
	}

	// ----

	// ---- Phase 3: Initial Sync: Verify validity ----


	err = BlockchainVerifyValidity(IAmFullNode, false) // also affects statedb
	if err != nil {
		logger.L.Panicf("Chaindb or statedb is NOT valid. It is recommended that you restart your node with an Initial sync! Error: %v", err)
	} 
	
	logger.L.Printf("\nSuccessfully verfied validity of chaindb and built statedb.\n")

	// set amount of required data confirmations back to initial value that was passed from main.go
	SyncHelper.ConfirmationsReqWrite(initialConfReq)

	// ---- Performance stat reporting ----

	// report performance stat for Event_InitialSyncChaindbVerified
	// 		get current time
	curTimeRightNow = time.Now().UnixNano()
	//		construct stat to be sent
	pStat := monitoring.NewPerformanceData(curTimeRightNow, MyDockerAlias, monitoring.Event_InitialSyncChaindbVerified, "foo") // no field can be empty so put bogus message
	// 		post stat
	err = monitoring.SendPerformanceStatWithRetries(pStat)
	if err != nil {
		logger.L.Printf("Warning - Failed to report performance stat: %v", err)
	}

	// ----

	// only if you are not the RA, request any live data you might have missed during the time from your startup to this point directly from the RA
    if !IAmRA {
    	// change back to initial sync mode so that you can receive direct communication
    	if IAmFullNode {
			SyncHelper.NodeModeWrite(SyncMode_Initial_Full)
		} else {
			SyncHelper.NodeModeWrite(SyncMode_Initial_Light)
		}
		logger.L.Printf("Switch to initial sync mode to prepare receiving LiveData.")

		// request current LiveData from RA
		//			get RA peer ID by casting RA publicKey
		var err error
		raPeerID, err = peer.IDFromPublicKey(RApub)
		if err != nil {
			logger.L.Panic(err)
		}
		//			request data from RA directly (try sending request repeatedly until it works, if e.g. context timeout issues occur just try again). But only try again until you have hit the upper retry cap
		chatRetryCounter := 0
		err = SendViaChat([]byte("needlivedata"), raPeerID, h, ctx)
		for ((err != nil) && (chatRetryCounter < directChatRetryAmountUpperCap)) {
			logger.L.Printf("Failed to request Live Data from RA directly during my initial sync: %v\nWill try again.", err)

			// try again
			RandomShortSleep()
			chatRetryCounter += 1
			err = SendViaChat([]byte("needlivedata"), raPeerID, h, ctx)
		}
		// either it worked (no error) or no more retries are allowed
		if err != nil {
			logger.L.Panicf("Even after retrying %v times I was not able to request Live Data from the RA via direct chat. This makes me want to panic!!!", directChatRetryAmountUpperCap)
		}
		logger.L.Printf("Successfully sent Live Data request to RA")

		// sleep until live data is received
		for {
			if !didReceiveLiveData {
				time.Sleep(10 * time.Millisecond)
			} else { // data has been received
				//logger.L.Printf("Live Data has been received.")
				break
			}
		}

	}

	// ---- End of initial sync ----

	// Phase 4: Switch to Continuous sync mode
	switch OriginalMode {
	case SyncMode_Initial_Full, SyncMode_Continuous_Full:
		SyncHelper.NodeModeWrite(SyncMode_Continuous_Full)
	case SyncMode_Initial_Light, SyncMode_Continuous_Light:
		SyncHelper.NodeModeWrite(SyncMode_Continuous_Light)
	case SyncMode_Initial_Mine, SyncMode_Continuous_Mine:
		SyncHelper.NodeModeWrite(SyncMode_Continuous_Mine)
	//default:
	//	logger.L.Printf("You are in passive sync mode.")
	}

	finalSyncMode := SyncHelper.NodeModeGet()
	logger.L.Printf("Reached final sync mode: %v", finalSyncMode.String())

	// ---- Performance stat reportinng ----

	// report performance stat for Event_InitialSyncCompleted
	// 		get current time
	curTimeRightNow2 := time.Now().UnixNano()
	//		construct stat to be sent
	pStat2 := monitoring.NewPerformanceData(curTimeRightNow2, MyDockerAlias, monitoring.Event_InitialSyncCompleted, "foo") // no field can be empty so put bogus message
	// 		post stat
	err = monitoring.SendPerformanceStatWithRetries(pStat2)
	if err != nil {
		logger.L.Printf("Warning - Failed to report performance stat: %v", err)
	}

	// ----

	// if you are a miner, start mining now but only if there is more than x seconds left to solve the problem
	minTimeLeftUntilProblemExpiry := uint64(30)
	curTime := block.GetCurrentTime()
	if finalSyncMode == SyncMode_Continuous_Mine {
		curSimtask := BlockProblemHelper.GetSimulationTask()
		if curSimtask.SimHeader.ExpirationTime > 0 && (curSimtask.SimHeader.ExpirationTime > curTime + minTimeLeftUntilProblemExpiry) {
			curSimtask.RunSimulation()
		} else {
			logger.L.Printf("Will not start mining right away because the current block problem expires soon.\n")
		}
	}

}

// ---- End of sync related code ----

// ---- BlockProblemHelperStruct ----

// BlockProblemHelperStruct is used to keep track of the current block problem and miner commitments. This will help for solution determination and winner selection procedures
type BlockProblemHelperStruct struct {
	RAcommitSecret          string          		// secretBytes that are only revealed by RA as soon as the block problem has expired
	Miners  				[]ActiveMiner 			// Keep track of which active miners broadcast which commitments for the current problem
	SimulationTask          simpar.SimulationTask 	// Keep track of all simtask details
}

// ActiveMiner is used to aggregate information about an active miner that has broadcast commitments before problem expiry.
type ActiveMiner struct {
	Commitment 					simsol.MinerCommitment 	// Commitment 1: Hash(solutionHash) that this miner broadcast, Commitment 2: Signature Sig(solutionHash) that this miner broadcast [solutionHash unknown at least until problem expires]
	ActualSolutionHashString	string 					// Only RA sets value for this
}

// NewActiveMiner is the constructor for ActiveMiner. RA will later add the actually receive solutionHash to the third field.
func NewActiveMiner(commitment simsol.MinerCommitment) ActiveMiner {
	return ActiveMiner {
		Commitment: commitment,
		ActualSolutionHashString: "",
	}
}


// GetBlockID gets the current blockID from the BlockProblemHelperStruct (ID of block that will be created after current problem is solved)
func (b *BlockProblemHelperStruct) GetBlockID() uint32 {
	networkingMutex.RLock()
	defer networkingMutex.RUnlock()

	return b.SimulationTask.SimHeader.BlockID
}


// GetProblemID gets the current problemID from the BlockProblemHelperStruct (unique hash of currently active block problem)
func (b *BlockProblemHelperStruct) GetProblemID() hash.Hash {
	networkingMutex.RLock()
	defer networkingMutex.RUnlock()

	return b.SimulationTask.ProblemHash
}


// GetProblemExpirationTime gets the current problem expiration time from the BlockProblemHelperStruct
func (b *BlockProblemHelperStruct) GetProblemExpirationTime() uint64 {
	networkingMutex.RLock()
	defer networkingMutex.RUnlock()

	return b.SimulationTask.SimHeader.ExpirationTime
}

// GetRAcommit gets the RAcommit from the BlockProblemHelperStruct (Keccak256(PrevBlockHash + secretBytes) that RA commits to before sending out new block problem)
func (b *BlockProblemHelperStruct) GetRAcommit() hash.Hash {
	networkingMutex.RLock()
	defer networkingMutex.RUnlock()

	return b.SimulationTask.SimHeader.RAcommit
}

// SetRAcommitSecret sets the RAcommitSecret for the BlockProblemHelperStruct
func (b *BlockProblemHelperStruct) SetRAcommitSecret(raCommitSecret string) {
	networkingMutex.Lock()
	defer networkingMutex.Unlock()

	b.RAcommitSecret = raCommitSecret

	if raCommitSecret == "" {
		logger.L.Printf("RAcommitSecret has been reset")
	} else {
		logger.L.Printf("RAcommitSecret has been set: %v\n", raCommitSecret)
	}
	
}

// GetRAcommitSecret gets the RAcommitSecret from the BlockProblemHelperStruct
func (b *BlockProblemHelperStruct) GetRAcommitSecret() string {
	networkingMutex.RLock()
	defer networkingMutex.RUnlock()

	return b.RAcommitSecret
}

// SetSimulationTask sets the current SimulationTask for the BlockProblemHelperStruct
func (b *BlockProblemHelperStruct) SetSimulationTask(simTask simpar.SimulationTask) {
	networkingMutex.Lock()
	defer networkingMutex.Unlock()

	b.SimulationTask = simTask

	// setting an empty simtask is equal to resetting the current simtask
	if (len(b.SimulationTask.ProblemHash.Bytes) == 0) {
		logger.L.Printf("Empty SimulationTask has been set\n") // could e.g. happen when RA sends you live data before it ever had created a block problem
	} else {
		logger.L.Printf("SimulationTask with unique ID %v has been set\n", simTask.ProblemHash.GetString())
	}

}

// GetSimulationTask gets the current SimulationTask from the BlockProblemHelperStruct
func (b *BlockProblemHelperStruct) GetSimulationTask() simpar.SimulationTask {
	networkingMutex.RLock()
	defer networkingMutex.RUnlock()

	return b.SimulationTask
}

// AddMiner is used to add a new ActiveMiner to the Miners slice in a concurrency-safe way.
// If the miner is already stored in the slice then it is not added again (only first received broadcast is accepted)
func (b *BlockProblemHelperStruct) AddMiner(miner ActiveMiner, skipTimeCheck bool) error {
	networkingMutex.Lock()
	defer networkingMutex.Unlock()

	// ensure miner commitment was received before the problem expired
	// This check only is done when you received the commitment in real-time. It would create issues when during initial sync you request current state of BPH and then when adding the miners locally you would pretend these commitments were received just now..
	if !skipTimeCheck {
		curTime := block.GetCurrentTime()
		knownExpirationTime := b.SimulationTask.SimHeader.ExpirationTime
		if curTime > knownExpirationTime {
			return fmt.Errorf("AddMiner - Miner with ID %v tried to commit at time %v but the problem expired at %v. Ignoring commitment.\n", miner.Commitment.OriginalSenderNodeID, curTime, knownExpirationTime)
		}	
	}

	// do not add miner if it already has committed to this problem (only first broadcast is valid)
	for _, m := range b.Miners {
		if m.Commitment.OriginalSenderNodeID == miner.Commitment.OriginalSenderNodeID {
			return fmt.Errorf("AddMiner - Miner with ID %v already commited. Ignoring its new broadcast.\n", miner.Commitment.OriginalSenderNodeID)
		}
	}

	// add miner to slice
	b.Miners = append(b.Miners, miner)

	logger.L.Printf("Appended miner %v who broadcast a commitment to the slice", miner.Commitment.OriginalSenderNodeID)

	return nil
}

// GetMiners is used to access the Miners slice in a concurrency-safe way.
func (b *BlockProblemHelperStruct) GetMiners() []ActiveMiner {
	networkingMutex.RLock()
	defer networkingMutex.RUnlock()

	return b.Miners
}

// ResetMiners is used to reset the list of miners that have broadcast a mining commitment. Used after a new block has been received.
func (b *BlockProblemHelperStruct) ResetMiners() error {
	networkingMutex.Lock()
	defer networkingMutex.Unlock()

	// reset miners slice
	b.Miners = []ActiveMiner{}

	logger.L.Printf("ResetMiners - []ActiveMiner slice has been reset\n")

	return nil
}

// CheckIfCommitmentAvailable checks whether a given nodeID has broadcast a seemingly valid commitment yet.
// Returns true if commitment is already available, returns false otherwise.
func (b *BlockProblemHelperStruct) CheckIfCommitmentAvailable(nodeID string) bool {
	networkingMutex.RLock()
	defer networkingMutex.RUnlock()

	commitmentAvailable := false 
	for _, m := range b.Miners {
		if m.Commitment.OriginalSenderNodeID == nodeID {
			commitmentAvailable = true
		}
	}

	return commitmentAvailable
}

// GetCommitment retrieves the commitment of a miner of interest and returns it. Will panic if it doesn't exist so only only use this function after ensuring with CheckIfCommitmentAvailable().
func (b *BlockProblemHelperStruct) GetCommitment(nodeID string) simsol.MinerCommitment {
	networkingMutex.RLock()
	defer networkingMutex.RUnlock()

	for _, m := range b.Miners {
		if m.Commitment.OriginalSenderNodeID == nodeID {
			return m.Commitment
		}
	}

	logger.L.Panicf("GetCommitment - Commitment of miner %v should be available but could not be accessed!", nodeID) // might as well use Errorf doesn't matter I guess
	return simsol.MinerCommitment{} // fixes a go bug (well not really, they define the built-in panic to terminate but not the logger panic) where it doesnt understand that nothing after a logger panic can be executed
}

// DetermineEligibleMiners is called when problem expires to loop over all commitments and determine all miners that are eligible for winner selection.
// This function will perform two filtering stages, first subsolution matching (which is not yet implemented) and then it determines the most common solution. All miners with valid commitments that pass both stages are eligible for winner selection.
// Note: The RA is able to verify the commitment signatures before even adding a miner to the ActiveMiner slice, that's why it does not again have to check the validity of the commitment signatures (but all other nodes have to do it after receiving a new block from RA).
func (b *BlockProblemHelperStruct) DetermineEligibleMiners(acceptedSolutionHash string) []ActiveMiner {
	networkingMutex.RLock()
	defer networkingMutex.RUnlock()

	// if subsolution matching is implemented add all of this here

	countMap := make(map[string]uint)

	if len(b.Miners) == 0 {
		return []ActiveMiner{} // case: no one was able to solve the block problem
	}

	// 0. miners only: filter out activeminers that did not sign the accepted value (RA already filtered so no need to do it again)
	var validSigActiveMiners []ActiveMiner
	if !IAmRA {
		hashOfAcceptedSolutionHashBytes := hash.HashStringGetBytes(acceptedSolutionHash) // this is the value honest miners would have put as HashCommit

		// determine which of the miners that broadcast a commitment signed the accepted solution hash that was accepted by RA
		for _, m := range b.Miners { 
			// get OriginalSender public key
			pub, err := NodeIDStringToPubKey(m.Commitment.OriginalSenderNodeID)
			if err != nil {
				logger.L.Printf("DetermineEligibleMiners - ActiveMiner NodeID %v can not be casted to Pubkey: %v", m.Commitment.OriginalSenderNodeID, err)
				continue
			}
			// check whether that node signed the from RA accepted solutionHash
			
			sigIsValid, err := pub.Verify(hashOfAcceptedSolutionHashBytes, m.Commitment.SigCommit)
			if err != nil {
				logger.L.Printf("DetermineEligibleMiners - ActiveMiner NodeID %v signature can not be verified due to: %v", m.Commitment.OriginalSenderNodeID, err)
				continue
			}
			if !sigIsValid {
				logger.L.Printf("DetermineEligibleMiners - ActiveMiner NodeID %v signature is invalid! This might be a malicious miner who tried to pretend to have solved the block problem but who did not. Since this node did not know the correction solution hash it might have signed some other value instead. Will be excluded from winner selection and not affect mostCommonSolution determination.", m.Commitment.OriginalSenderNodeID)
				continue
			} else { // sig is valid so this node qualifies
				validSigActiveMiners = append(validSigActiveMiners, m)
			}
			
		}
	} else { // RA already filtered so no need to do it again
		validSigActiveMiners = b.Miners
	}

	// 1. determine most common solution
	for _, m := range validSigActiveMiners {
		// get hash of solution hash of miner
		curHash := m.Commitment.HashCommit.GetString()
		
		_, alreadyExists := countMap[curHash]
		if alreadyExists {
			countMap[curHash]++ // increase amount of occurances by 1
		} else {
			countMap[curHash] = 1
		}
	}

	// 2. get most common commit solution hash (multiple, different solutions might have same amount of occurrences)
	var mostCommonSolutionHashes []string
	maxInt := uint(0) 

	for s, i := range countMap {
		if i > maxInt {
			// set new max value
			maxInt = i

			// empty slice (all hashes there need to be removed), then add current hash
			mostCommonSolutionHashes = []string{s} // alternative solution: mostCommonSolutionHashes = append(mostCommonSolutionHashes[:0], s)

		} else if i == maxInt {
			mostCommonSolutionHashes = append(mostCommonSolutionHashes, s) // add new hash that has current max value
		}
	}

	// 3. handle case where multiple different solutions have same max amount of occurrences
	var acceptedSolutionHashString string
	if len(mostCommonSolutionHashes) > 1 {
		// convert latestBlockHash to number, then calculate: latestBlockHashNumber mod len(mostCommonSolutionHashes) to get selected hash index
		//		1. get latestBlockHash
		latestBlockBytes, err := BlockGetBytesFromDb("latest")
		if err != nil {
			logger.L.Panic(err)
		}
		var latestBlockHash hash.Hash
		if IAmFullNode {
			_, latestBlockHash = FullBlockBytesToBlockLight(latestBlockBytes)
		} else {
			_, latestBlockHash = HeaderBytesToBlockLight(latestBlockBytes)
		}

		// 		2. convert hash to uint64 number
		latestBlockHashNumber := HexStringToNumber(latestBlockHash.GetString())
		// 		3. calculate latestBlockHashNumber mod len(mostCommonSolutionHashes) to get selected hash index
		acceptedSolutionHashIndex := latestBlockHashNumber % uint64(len(mostCommonSolutionHashes))
		//		4. sort mostCommonSolutionHashes ascending (modified in place)
		SortStringSliceAscending(mostCommonSolutionHashes)
		//		5. use the index to selected the accepted solution hash
		acceptedSolutionHashString = mostCommonSolutionHashes[acceptedSolutionHashIndex]
	} else if len(mostCommonSolutionHashes) == 1{ // case: the choice of the most common commit hash is trivial because there is exactly one with highest map int
		acceptedSolutionHashString = mostCommonSolutionHashes[0]
	} // else acceptedSolutionHashString stays on its zero value ""

	// 4. get list of eligible miners (that uploaded commitment which includes the accepted solution hash)
	var eligibleMiners []ActiveMiner
	if acceptedSolutionHashString != "" {
		for _, m2 := range validSigActiveMiners {
			if m2.Commitment.HashCommit.GetString() == acceptedSolutionHashString {
				eligibleMiners = append(eligibleMiners, m2)
			}
		}	
	}

	return eligibleMiners
}

// UpdateActiveMinerWithActualSolution is used by RA only to store the actually received solution hash of a miner so that it can be retrieved later.
func (b *BlockProblemHelperStruct) UpdateActiveMinerWithActualSolution(nodeID string, actualSolutionHash string) {
	networkingMutex.Lock()
	defer networkingMutex.Unlock()

	for index, m := range b.Miners {
		if m.Commitment.OriginalSenderNodeID == nodeID {
			b.Miners[index].ActualSolutionHashString = actualSolutionHash // do not modify m.NodeID as it is just a copy
			logger.L.Printf("UpdateActiveMinerWithActualSolution - Successfully set solutionHash of miner %v with value: %v\n", nodeID, b.Miners[index].ActualSolutionHashString)
		}
	}

}

// GetCopyWithoutSecret creates an instance of BlockProblemHelperStruct that contains all current fields but the RAcommitSecret is set to empty string.
// Used only by RA and only to get current state of BlockProblemHelper when a node that is about to finish its initial sync requests it.
func (b *BlockProblemHelperStruct) GetCopyWithoutSecret() BlockProblemHelperStruct {
	networkingMutex.RLock()
	defer networkingMutex.RUnlock()

	return BlockProblemHelperStruct {
		RAcommitSecret: "", // exclude this as it only is ever revealed before new block is published
		Miners: b.Miners,
		SimulationTask: b.SimulationTask,
	}
}

// ---- End of BlockProblemHelperStruct ----

// ChainDBRequest is the struct used to request specific chaindb data from other nodes via the topic 'pouw_chaindb'
type ChainDBRequest struct {
	OriginalSenderNodeID  			string 	// Retain info who originally requested the data so that nodes later can directly start communication with this node via chat protocol
	WantOnlyAllBlockHashes 			bool 	// If you want []string (a list of blockHashes) set this to true, in this case the following fields are ignored
	WantFullBlock 					bool 	// Info whether you are interested in block.Block (you are a full node) or block.Header (you are a light node)
	WantOnlyLatestBlock 			bool    // Info whether you are only interest in the newest block (if this is set to true, the next field is ignored)
	BlockHashStringOfInterest     	string 	// Info which blockHash data you are interested in. If it does not exist you might not get a reply.
}

// NewChainDBRequest is the constructor of ChainDBRequest. I like using constructors so that when the struct is ever changed by e.g. adding fields the code breaks (and I get notified) instead of silently putting zero values for the new fields which can be hard to detect.
func NewChainDBRequest(originalSenderNodeID string, wantOnlyAllBlockHashes bool, wantFullBlock bool, wantOnlyLatestBlock bool, blockHashStringOfInterest string) ChainDBRequest {
	return ChainDBRequest {
		OriginalSenderNodeID: originalSenderNodeID,
		WantOnlyAllBlockHashes: wantOnlyAllBlockHashes,
		WantFullBlock: wantFullBlock,
		WantOnlyLatestBlock: wantOnlyLatestBlock,
		BlockHashStringOfInterest: blockHashStringOfInterest,
	}
}

// LiveData is a struct used to transmit current live data state to nodes that just completed their initial sync.
// E.g. a new node knows about all block hashes and the content of the latest block and now requests LiveData to be informed about currently pending transactions and the currently active block problem.
type LiveData struct {
	BPH 				BlockProblemHelperStruct
	PendingTransactions []transaction.Transaction
}

// NewLiveData is the constructor for LiveData.
func NewLiveData(b BlockProblemHelperStruct, t []transaction.Transaction) LiveData {
	return LiveData {
		BPH: b,
		PendingTransactions: t,
	}
}

// ---- Direct communication via chat ----

// SendViaChat is used to send a message directly to a peer (no topic involved). It takes a message (usually wrapped in TSStruct), a peerID, a host and a context and sends it via libp2p's /chat/1.0 protocol to the set recipient.
// Returns nil if everything went smoothly, otherwise returns an error message.
func SendViaChat(data []byte, targetPeer peer.ID, h host.Host, ctx context.Context) error {
	//		send solution directly
	chatStream, err := h.NewStream(ctx, targetPeer, "/chat/1.0")
    if err != nil {
        return fmt.Errorf("SendViaChat - ERROR: Failed to open stream to target peer %v, message was NOT sent: %v\n", targetPeer.String(), err) // if attempt to connect to peer fails, don't try to send the message
    }
	defer chatStream.Close()

	// the message you send should end with a newline, otherwise on the receiving end nothing is received (due to use of Reader) [actually test again whether it still is required]
	data = append(data, []byte("\n")...)	// this is the []byte equivalent of string where you do += "\n"

	// try to send data
	_, err = chatStream.Write(data)
	
	return err // only nil when everything worked
}

// HandleIncomingChatMessage is used to handle directly incoming data. It receives incoming messages on registered protocol (chat1.0) and reads until newline.
// Note: If the sent message does not have a newline \n, then nothing will be received on the receiving end, so when sending a message make sure to append \n to the end.
func HandleIncomingChatMessage(chatStream network.Stream, h host.Host, ctx context.Context) {
	defer chatStream.Close()

	// determine nodeID of sender (so that its public key can be derived for signature verification / or just access the public key directly)
	senderPeerID := chatStream.Conn().RemotePeer()
	senderNodeID := senderPeerID.String()	// RemotePeer() returns peer.ID, on that you can call String() which returns string version of this ID, used to determine whether that node already sent data for this request 
	senderNodePubKey := chatStream.Conn().RemotePublicKey()

	// if you are RA, then you are interested in simSol
	if IAmRA {
		reader := bufio.NewReader(chatStream)
		receivedMessage, err := io.ReadAll(reader)
		if err != nil {
			logger.L.Printf("HandleIncomingChatMessage - RA failed to read chat message: %v. The message was sent by node %v\n", err, senderNodeID)
			return
		}

		// ---- Start of handling Live Data requests ----

		// reply with live data (blockproblemhelper + pending transactions) when request is "needlivedata\n" (this msg is sent by node after finishing initial sync)
		needLiveDataBytes := []byte("needlivedata")
		needLiveDataBytesWithNewline := append(needLiveDataBytes, []byte("\n")...)

		if string(receivedMessage) == string(needLiveDataBytesWithNewline) {
			// get copy of current blockproblem helper (with revealing secret bytes early)
			copiedBPH := BlockProblemHelper.GetCopyWithoutSecret()
			// retrieve pending transaction slice
			pendTrans := GetPendingTransactionSlice()
			// put it both in LiveData struct instance
			liveData := NewLiveData(copiedBPH, pendTrans)

			// serialize it
			liveDataBytes, err := msgpack.Marshal(&liveData)
			if err != nil {
				logger.L.Panic(err)
			}
			// wrap in TS
			ldWrappedBPH := NewTransportStruct(TSData_LiveData, senderNodeID, liveDataBytes)
			// send reply to node (if it fails retry but only up to upper cap often)
			chatRetryCounter := 0
			err = SendViaChat(ldWrappedBPH, senderPeerID, h, ctx)
			for ((err != nil) && (chatRetryCounter < directChatRetryAmountUpperCap)) {
				logger.L.Printf("RA - Failed to send requested Live Data to node %v: %v\nWill try again..", senderNodeID, err)

				// try again
				RandomShortSleep()
				chatRetryCounter += 1
				err = SendViaChat(ldWrappedBPH, senderPeerID, h, ctx)
			}
			// either it worked (no error) or no more retries are allowed
			if err != nil { // the RA should NOT panic if it fails to direct chat with some random node, so use Printf instead of Panicf
				logger.L.Printf("Even after retrying %v times I was not able to send the requested Live Data to node %v via direct chat. I will not panic because I am the RA.", directChatRetryAmountUpperCap, senderNodeID)
			} else {
				logger.L.Printf("Successfully sent the requested Live Data to node %v via direct chat.", senderNodeID)
			}

			return // nothing more to do
		}

		// ---- End of handling Live Data requests ----

		// determine type of data that was sent (RA only cares about simsol)
		dataTypeID, err := TransportStructGetDataID(receivedMessage)
		if err != nil {
			logger.L.Printf("TransportStructGetDataID - RA failed to unmarshal: %v which was sent by node %v\n", err, senderNodeID)
			return
		}

		if dataTypeID == TSData_SimSol {
			//logger.L.Printf("RA received a miner's uploaded solution. Will now try to handle received data..")
			go RAReceivedSimSol(receivedMessage, senderNodePubKey, senderNodeID) // goroutine must handle this to avoid wait-locking
		} else {
			logger.L.Printf("HandleIncomingChatMessage - RA expected data of type TSData_SimSol but received %v from node %v which RA is not interested in, ignoring it. \n", dataTypeID, senderNodeID)
			// if you implement reputation system of nodes, negatively affect reputation of nodes that spam data
		}

		return
	}

	// get current sync mode
	curNodeMode := SyncHelper.NodeModeGet()

	// if not in initial mode, then ignore any direct connections (technically accepts them and then does nothing) EXCEPT for when they are sent by the RA
	if ((curNodeMode != SyncMode_Initial_Full) && (curNodeMode != SyncMode_Initial_Light) && (curNodeMode != SyncMode_Initial_Mine)) && (senderNodeID != RANodeID) {
		logger.L.Printf("I received direct connection data from node %v but I am not in initial sync mode so I will ignore it.", senderNodeID)
		return
	}
	
	// use bufio.Reader to read from stream
	reader := bufio.NewReader(chatStream)
	receivedMessage, err := io.ReadAll(reader)	// https://pkg.go.dev/io#ReadAll
	if err != nil {	// EOF is not an error here
		logger.L.Printf("HandleIncomingChatMessage - Error reading message: %v. This message was sent by node %v\n", err, senderNodeID)
		return
	}
	
	// determine type of data that was sent (function returns TSData which is int alias)
	dataTypeID, err := TransportStructGetDataID(receivedMessage)
	if (err != nil) || (int(dataTypeID) == 0) { // 0 is nil of int (and of TSData), so there must have gone sth wrong. Invalid enum values would also lead to err != nil
		logger.L.Printf("TransportStructGetDataID - Failed to unmarshal: %v. This data was sent by node %v\n", err, senderNodeID)
		return
	}

	// check whether the RA has sent you the current live data
	if senderNodeID == RANodeID {
		if dataTypeID == TSData_LiveData {
			// extract TS
			ldSer, _, err := TransportStructExtraction(TSData_LiveData, receivedMessage, true)
			if err != nil {
				logger.L.Panic(err)
			}
			// deserialize
			var recLD LiveData
			err = msgpack.Unmarshal(ldSer, &recLD)
			if err != nil {
				logger.L.Panic(err)
			}


			// handle BPH part of live data
			//		set received SimulationTask
			BlockProblemHelper.SetSimulationTask(recLD.BPH.SimulationTask)
			//		set active Miners slice
			//				first reset old one (just in case it's not empty for some reason)
			err = BlockProblemHelper.ResetMiners()
			if err != nil {
				logger.L.Panicf("Failed to reset Miner slice of BPH due to error: %v", err)
			}
			//				add newly received Miners you might have missed during initial sync
			for _, m := range recLD.BPH.Miners {
				err = BlockProblemHelper.AddMiner(m, true) // skip time check because the RA has received these commitments earlier (you just happen to be late and add them retrospectively)
				if err != nil {
					logger.L.Panicf("Failed to add Miner to BPH Miner slice due to error: %v", err)
				} else {
					logger.L.Printf("Successfully added new miner to BPH. Miner address is: %v", m.Commitment.OriginalSenderNodeID)
				}
			}
			// 		set received RAcommitSecret is always empty anyways but can't hurt to explicitely set it
			BlockProblemHelper.SetRAcommitSecret("")



			// handle transaction part of live data
			// 		add transactions to your pending slice (duplicates that you might have received in the mean time until RA replied will be skipped)
			for _, t := range recLD.PendingTransactions {
				AddPendingTransaction(t)
			}


			// notify sync function that you received the live data
			didReceiveLiveData = true

			logger.L.Printf("Live Data has successfully affected BPH and PendTrans.\n")
			return
		} 

	}

	// filter out data that you don't care about depending on which sync mode you are in
	dataIsOfInterest := AmIInterestedInThisDirectChatData(curNodeMode, dataTypeID)
	if !dataIsOfInterest {
		logger.L.Printf("HandleIncomingChatMessage - Received data that I am not interested in. I am in NodeMode %v but I received TSData %v which I do not care about currently.", curNodeMode, dataTypeID)
		return
	}

	// unpack received TransportStruct (data is declined if sig invalid or if contained data type does not match announced data type)
	recData, recDataHash, err := TransportStructExtraction(dataTypeID, receivedMessage, false)
	if err != nil || recDataHash == "" {
		logger.L.Printf("HandleIncomingChatMessage - Invalid data received or invalid signature detected for ID %v: %v with hash: %v\n", dataTypeID, err, recDataHash)
		return
	}

	// it is now known that the sent data is in a valid format and of interest to us
	// so now handle the received data in a way that is defined by both the current SyncMode and the data type that is received

	switch curNodeMode {
	// case: Initial sync (technically we already know that we are in initial mode here, but its more robust to changes to explictely have it switched again)
	case SyncMode_Initial_Full, SyncMode_Initial_Light, SyncMode_Initial_Mine:
		switch dataTypeID {
		// case: block.Block
		case TSData_Block:
			logger.L.Printf("Serialized full block received.")
			SyncHelper.MapAdd(recData, recDataHash, senderNodeID)
		// case: block.Header
		case TSData_Header:
			logger.L.Printf("Serialized header received.")
			SyncHelper.MapAdd(recData, recDataHash, senderNodeID)
		// case: []string
		case TSData_StringSlice:
			logger.L.Printf("Serialized []string (of blockHashes) received.")
			// it is known already that the data can be safely unpacked, so unpack it to print which block hashes you received
			recDataBlockHashSlice, err := UnpackSerializedStringSliceBlockHashes(recData)
			if err != nil {
				errMsg := fmt.Sprintf("UnpackSerializedStringSliceBlockHashes - Error trying to unpack string slice even though it already passed TSExtraction test: %v \n", err)
				logger.L.Panic(errMsg) // this can only happen if UnpackSerializedStringSliceBlockHashes fails which should never happen here 
			}

			logger.L.Printf("Received these blockhashes:")
			for _, abc := range recDataBlockHashSlice {
				logger.L.Printf(abc)
			}
			
			// add the marshaled []byte data to the map of the SyncHelper
			SyncHelper.MapAdd(recData, recDataHash, senderNodeID)

		}
	}

}

// ---- Helper functions ----

// waitForData repeatedly checks whether a piece of received data has been verified often enough and signals with true when the data is confirmed to be valid. 
// This is only used to initially get all blockHashes. If the blockHashes are known, there is no need for the ConfirmatedReq stuff.
func waitForData(acceptedDataChannel chan []byte) {
	// run the following code every 20 ms
    for range time.Tick(20 * time.Millisecond) {
		confirmationsReached, data, _ := SyncHelper.MapCheckIfConfirmationsReached()
        if confirmationsReached {	
			SyncHelper.MapReset()		// reset the map to prepare next data request process
            acceptedDataChannel <- data	// return []byte
			return // i dont think this can be reached but doesnt matter
        }

    }
}

// waitForDataWithHash keeps checking whether a requested piece of data has been received yet. When received, it returns that data via the the []byte channel acceptedDataChannel.
func waitForDataWithHash(expectedBlockHashString string, acceptedDataChannel chan []byte) {
    for {
    	// only 1 confirmation needed when you already know which hash you need
		confirmationsReached, data, hashString := SyncHelper.MapCheckIfConfirmationsReached()
		// ok we received block data
        if confirmationsReached {
			// check if the received data has the required hash
			if hashString == expectedBlockHashString {
				SyncHelper.MapReset()		// reset the map to prepare next data request process
				acceptedDataChannel <- data	// return []byte
				return // i dont think this can be reached but doesnt matter
			}
			logger.L.Printf("Received blockData with hash %v but we were looking for data with hash %v. Ignoring.. \n", hashString, expectedBlockHashString)
			SyncHelper.MapReset()

        }
		time.Sleep(20 * time.Millisecond)	// checking too often is problematic cuz of mutex locking, so short timeout needed
    }
}


// waitForCommitment repeatedly checks whether a valid commitment of a specified miner address has successfully been received for a given problem.
// Returns true if valid commitment is found in time, return false when the current block problem changes before a valid commitment was received.
func waitForCommitment(commitmentAvailable chan bool, relevantProblemID string, minerAddress string) {
    for {
    	// if no valid commitment was received before the block problem changes, return false
    	currentProblemID := BlockProblemHelper.GetProblemID().GetString()
    	if currentProblemID != relevantProblemID {
    		commitmentAvailable <- false
    		return // i dont think this can be reached but doesnt matter
    	}

    	// check if valid commitment has been received
		commitmentIsNowAvailable := BlockProblemHelper.CheckIfCommitmentAvailable(minerAddress)
        if commitmentIsNowAvailable {	
            commitmentAvailable <- true
			return // i dont think this can be reached but doesnt matter
        }

        // if not, wait a bit and check again
		time.Sleep(2 * time.Second)	// checking every 2 seconds is often enough
    }
}

// RandomShortSleep is used to sleep for a short amount (between 5 and 15 milliseconds). It is used to retry something networking-related after a short delay.
func RandomShortSleep() {
	min := 5
	max := 15

	// seed RNG (doesn't have to be cryptographically secure)
	unsafeRand.Seed(time.Now().UnixNano())

	// get random int in [min,max] which is inclusive (min or max could be chosen)
	randInt := unsafeRand.Intn(max-min+1) + min

	// sleep this many milliseconds (you also need to cast int to time.Duration)
	time.Sleep(time.Duration(randInt) * time.Millisecond)
}

// RemoveDuplicateStrings removes duplicate strings from []string and return the cleansed slice that does not contain any duplicates.
func RemoveDuplicateStrings(ss []string) []string {
	seenAlready := make(map[string]bool)
	var sliceWithoutDuplicates []string

	for _, s := range ss {
		// add this string to the map if it was not seen before, in this case also add it to sliceWithoutDuplicates
		if _, ok := seenAlready[s]; !ok {
			seenAlready[s] = true
			sliceWithoutDuplicates = append(sliceWithoutDuplicates, s)
		}
	}

	return sliceWithoutDuplicates
}

// AmIInterestedInThisDirectChatData checks whether a node in a given sync mode is interested of received data of a given type.
// Returns true if the data is of interest, returns false if the data should be ignored.
func AmIInterestedInThisDirectChatData(syncMode Mode, recDataType TSData) bool {
	switch syncMode {
	// Passive modes do not accept incoming direct chat data at all
	case SyncMode_Passive_Full, SyncMode_Passive_Light:
		return false

	// Initial_Full, SyncMode_Continuous_Full, Initial_Mine and SyncMode_Continuous_Mine only care about []string and block.Block
	case SyncMode_Initial_Full, SyncMode_Continuous_Full, SyncMode_Initial_Mine, SyncMode_Continuous_Mine:
		if (recDataType != TSData_StringSlice) && (recDataType != TSData_Block) {
			return false
		} else {
			return true
		}
	// Initial_Light and  Continuous_Light only care about []string and block.Header
	case SyncMode_Initial_Light, SyncMode_Continuous_Light:
		if (recDataType != TSData_StringSlice) && (recDataType != TSData_Header) {
			return false
		} else {
			return true
		}

	default:
		logger.L.Printf("AmIInterestedInThisDirectChatData - Default case: I do not seem to be interested in this data")
		return false
	}

}

// WinnerSelection takes a slice of elgible miners and the RAcommitSecret and then performs the winner selection after the block problem has expired and RA has published its secretCommitBytes.
// Returns the nodeID of the winner node.
func WinnerSelection(inputEligibleMiners []ActiveMiner, raCommitSecret string) string {
	// ensure that you actually received the raCommitSecret (if it still has its zero value you never got the message and now can't continue)
	if raCommitSecret == "" {
		// TODO: you should be able to recover from this by requesting it again from many nodes until x confirmations of same reply. however, currently not that easy because nodes quickly forget the previous RA commit to prepare for next problem (entire BPH is reset). So this would require multiple changes
	
		// not ideal but for now just panic, it means pubsub delivery did not work for an important message
		logger.L.Panicf("WinnerSelection - My raCommitSecret is still an empty string! This means I never got the message so I can not perform the winner selection. Will panic now, sorry!")
	}

	// store concated sigs
	c := ""

	//	0. sort list by nodeID ascending
	//			0.1 extract nodeID strings
	var nodeIDStrings []string
	for _, m := range inputEligibleMiners {
		nodeIDStrings = append(nodeIDStrings, m.Commitment.OriginalSenderNodeID)
	}

	//			0.2 sort string slice in place (ascending), 0<9<a<z and case-insensitive
	sort.Slice(nodeIDStrings, func(i, j int) bool {
		return strings.ToLower(nodeIDStrings[i]) < strings.ToLower(nodeIDStrings[j])
	})

	//			0.3 re-create active miner slice in the correct order + concatenate sigs in this order
	var eligibleMinersSorted []ActiveMiner
	for _, n := range nodeIDStrings {
		for _, o := range inputEligibleMiners {
			if n == o.Commitment.OriginalSenderNodeID {
				eligibleMinersSorted = append(eligibleMinersSorted, o)
				c += string(o.Commitment.SigCommit)
			}
		}
	}

	//  1. sigs have already been concatenated
	//	2. take secretCommitBytes (revealed by RA) and calculate Keccak256(c + raCommitSecret)
	a := hash.NewHash(c + raCommitSecret).GetString()

	// 3. take first 15 chars of a and convert to decimal number
	aDec, err := strconv.ParseInt(a[:15], 16, 64)
	if err != nil {
		logger.L.Panic(err)
	}

	//	4. calculate winner index: hash_to_number_result % len(activeMinerList)
	winnerIndex := aDec % int64(len(eligibleMinersSorted))

	//	5. access winner with index
	winnerNodeAddress := eligibleMinersSorted[winnerIndex].Commitment.OriginalSenderNodeID

	return winnerNodeAddress
}
