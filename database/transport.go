package database

// transport.go takes care of networking related procedures, such as e.g. packing data in a way that allows the receiver to determine which data it might contain before deserializing the actual payload.

import (
	"encoding/hex"
	"fmt"

	"github.com/vmihailenco/msgpack/v5"

	"github.com/felix314159/gophy/block"
	"github.com/felix314159/gophy/block/hash"
	"github.com/felix314159/gophy/block/simpar"
	"github.com/felix314159/gophy/block/simsol"
	"github.com/felix314159/gophy/block/transaction"
	"github.com/felix314159/gophy/logger"
)

// TSData is an alias for int which describes which kind of data was put in a TransportStruct. It makes it easier for the receiving node to determine which data might have been sent.
type TSData int

// String implements the stringer interface so that the readable string value of enum element is printed in error messages instead of just its int.
func (e TSData) String() string {
	switch e {
	case TSData_Block:
		return "TSData_Block"
	case TSData_Header:
		return "TSData_Header"
	case TSData_StringSlice:
		return "TSData_StringSlice"
	case TSData_SimulationTask:
		return "TSData_SimulationTask" // TSData_SimPar was renamed
	case TSData_SimSol:
		return "TSData_SimSol"
	case TSData_MinerCommitment:
		return "TSData_MinerCommitment"
	case TSData_RAcommitSecret:
		return "TSData_RAcommitSecret"
	case TSData_ChainDBRequest:
		return "TSData_ChainDBRequest"
	case TSData_Transaction:
		return "TSData_Transaction"
	case TSData_LiveData:
		return "TSData_LiveData"
	default:
		return fmt.Sprintf("%d", e)
	}
}

// ---- TransportStruct -----

// TransportStruct is a struct that contains serialized data and an enum element (TSData) that tells the receiver which kind of data it contains.
// It also contains a Sig field which holds the signature of the data that is contained in this struct to prevent tampering.
// The databases contain TransportStruct.Data, so this struct is specifically made for easier handling of untrusted data received from other nodes.
type TransportStruct struct {
	DataID					TSData	// Describes which kind of data is sent
	OriginalSenderNodeID 	string // Pubsub makes it difficult to later determine who the original sender of a received message was, so include this info manually. This will be converted to pubkey so that then the signature can be verified.
	Data					[]byte	// serialized msgpack object that contains actual data (msgpack allows double serialization)
	Sig						[]byte	// You hash the Data field with hash.NewHash and then you sign the resulting hash
}

// NewTransportStruct is the constructor for TransportStruct.
// Note that it returns the msgpack serialized instance of the created struct to simplify networking calls.
func NewTransportStruct(dataID TSData, originalSenderNodeID string, data []byte) []byte {
	// hash the data and access Bytes
	dataHash := hash.NewHash(string(data)).Bytes
	
	// sign hash of message using private key
	sig, err := PrivateKey.Sign(dataHash)
	if err != nil {
		logger.L.Panic(err) // signing a message should never fail
	}

	// create struct instance
	t := TransportStruct {
		DataID:					dataID,
		OriginalSenderNodeID: 	originalSenderNodeID,
		Data:					data,
		Sig:					sig,
	}

	// serialization
	tSer, err := msgpack.Marshal(&t)
	if err != nil {
		logger.L.Panic(err)
	}

	return tSer

}

// TransportStructGetDataID takes a serialized TransportStruct, deserializes it and returns the enum value of DataID.
// This function is used by the receiver during networking to determine which data might have been sent (DataID still could have been lied about but this is handled in TransportStructExtraction).
// Int-to-DataType Mapping: 1 = block.Block, 2 = block.Header, 3 = []string.
func TransportStructGetDataID(tsSer []byte) (TSData, error) {
	var ts TransportStruct
	err := msgpack.Unmarshal(tsSer, &ts)
	if err != nil {
		// 0 is the nil value of int and TSData is just an alias for int
		return 0, fmt.Errorf("TransportStructGetDataID - Failed to deserialize data to TransportStruct instance: %v \n", err)
	}

	return ts.DataID, nil
}

// TransportStructExtraction takes the expected DataID (the data type you requested) and a serialized TransportStruct (contains data you received, includes metadata) and a bool (which when true says only accept the data if it was originally sent by the RA) and then determines five things.
// 1. It determines whether the provided serialized TransportStruct can be deserialized into a valid TransportStruct.
// 2. It determines whether the contained DataID matches the ID of the expected data.
// 3. It determines whether the contained data can successfully be deserialized into the object that corresponds to the contained DataID.
// 4. Determines the hash string of the contained data (block.Block gets its BlockHeader hashed, block.Header gets itself hashed, []string is msgpacked and string()-casted and hashed)
// 5. Verifies that the included signature is valid to prevent nodeID spoofing.
// On success, this function returns the contained data as []byte, its hash as string [note: not necessarily the hash of the entire data: e.g. a full block would return the hash of its header only] and error nil. On failure nil, empty string and the error message are returned (don't panic just because a node sent you unexpected or invalid data).
func TransportStructExtraction(expectedDataID TSData, tsSer []byte, onlyAllowRAoriginalSender bool) ([]byte, string, error) {
	// try to deserialize tsSer into a TransportStruct instance
	var ts TransportStruct
	err := msgpack.Unmarshal(tsSer, &ts)
	if err != nil {
		logger.L.Printf("DEBUG EOF ERROR - THIS DATA LED TO ERROR: %v\n", tsSer)
		return nil, "", fmt.Errorf("TransportStructExtraction - Failed to deserialize data to TransportStruct instance: %v \n", err)
	}

	if ts.OriginalSenderNodeID == "" {
		return nil, "", fmt.Errorf("TransportStructExtraction - Field OriginalSenderNodeID is empty! Declining data.")
	}

	// get pubkey of ORIGINAL sender node ID
	originalSenderNodePubKey, err := NodeIDStringToPubKey(ts.OriginalSenderNodeID)
	if err != nil {
		return nil, "", fmt.Errorf("TransportStructExtraction - Failed to convert sender node id string to pubkey! Declining data due to error: %v \n", err)
	}

	// dataID matching
	foundDataID :=  ts.DataID
	if foundDataID != expectedDataID {
		return nil, "", fmt.Errorf("TransportStructExtraction - Expected DataID is %v but %v was found. Declining data.\n", expectedDataID, foundDataID)
	}

	// re-hash the data field (without looking what kind of bytes it consists of) for sig verification preparation and access Bytes field
	dataHash := hash.NewHash(string(ts.Data)).Bytes

	// verify signature (if signature is invalid data is instantly declined / error returned)
	//   	case 1: onlyAllowRAoriginalSender = true: try with RApub, if it doesnt work sig is invalid
	//		case 2: onlyAllowRAoriginalSender = false: try with pubkey of ts.OriginalSenderNodeID, if it doesnt work sig is invalid
	var verifiedSigSuccess bool
	if onlyAllowRAoriginalSender {
		verifiedSigSuccess, err = RApub.Verify(dataHash, ts.Sig)	// (hashThatWasSigned, ResultingSignature)
		if err != nil {
			return nil, "", fmt.Errorf("TransportStructExtraction - Failed to call verify signature function: %v. Declining data because it should have been signed by the RA but it was not. Given originalSenderNodeID is: %v \n", err, ts.OriginalSenderNodeID)
		}
	} else {
		verifiedSigSuccess, err = originalSenderNodePubKey.Verify(dataHash, ts.Sig)	// (hashThatWasSigned, ResultingSignature)
		if err != nil {
			return nil, "", fmt.Errorf("TransportStructExtraction - Failed to call verify signature function: %v. Declining data. Given originalSenderNodeID is: %v \n", err, ts.OriginalSenderNodeID)
		}
	}

	if !verifiedSigSuccess {
		logger.L.Printf("Invalid signature detected from someone who claims to have provided a message originally sent by node %v! This is the invalid sig: %v", ts.OriginalSenderNodeID, ts.Sig)
		return nil, "", fmt.Errorf("TransportStructExtraction - Invalid signature! Declining data by someone who claims to have provided a message originally sent by node %v \n", ts.OriginalSenderNodeID)
	}

	// try to deserialize contained data into expected datatype struct
	switch foundDataID {
	case TSData_Block:
		// try to deserialize into block.Block
		var blockData block.Block
		err = msgpack.Unmarshal(ts.Data, &blockData)
		if err != nil {
			return nil, "", fmt.Errorf("TransportStructExtraction - Failed to deserialize data into block.Block due to: %v. Declining data sent by node %v \n", err, ts.OriginalSenderNodeID)
		}
		
		// determine hash
		//		first msgpack relevant part
		msgpkdData, err := msgpack.Marshal(&blockData.BlockHeader)
		if err != nil {
			logger.L.Panic(err)
		}
		//		get hash string
		hashString := hash.NewHash(string(msgpkdData)).GetString()
	
		// note that i just check whether deserialization WOULD work and then return unchanged serialized data as []byte
		return ts.Data, hashString, nil


	case TSData_Header:
		// try to deserialize into block.Header
		var blockHeader block.Header
		err = msgpack.Unmarshal(ts.Data, &blockHeader)
		if err != nil {
			return nil, "", fmt.Errorf("TransportStructExtraction - Failed to deserialize data into block.Header %v. Declining data sent by node %v \n", err, ts.OriginalSenderNodeID)
		}
		
		// determine hash
		hashString := hash.NewHash(string(ts.Data)).GetString()
				
		// note that i just check whether deserialization WOULD work and then return unchanged serialized data as []byte
		return ts.Data, hashString, nil
		
		
	case TSData_StringSlice:
		// try to deserialize into []string
		var stringSlice []string
		err = msgpack.Unmarshal(ts.Data, &stringSlice)
		if err != nil {
			return nil, "", fmt.Errorf("TransportStructExtraction - Failed to deserialize data into a string slice %v. Declining data sent by node %v \n", err, ts.OriginalSenderNodeID)
		}
		
		// determine hash
		hashString := hash.NewHash(string(ts.Data)).GetString()
		
		// note that i just check whether deserialization WOULD work and then return unchanged serialized data as []byte
		return ts.Data, hashString, nil	
	case TSData_SimulationTask:
		var simTaskObject simpar.SimulationTask
		err := msgpack.Unmarshal(ts.Data, &simTaskObject)
		if err != nil {
			return nil, "", fmt.Errorf("TransportStructExtraction - Failed to deserialize data into simpar.SimulationTask due to error: %v. Declining data sent by node %v \n", err, ts.OriginalSenderNodeID)
		}

		// determine hash
		hashString := hash.NewHash(string(ts.Data)).GetString()

		return ts.Data, hashString, nil
	case TSData_SimSol:
		var blockProblemSolution simsol.BlockProblemSolution
		err := msgpack.Unmarshal(ts.Data, &blockProblemSolution)
		if err != nil {
			return nil, "", fmt.Errorf("TransportStructExtraction - Failed to deserialize data into simsol.BlockProblemSolution due to error: %v. Declining data sent by node %v \n", err, ts.OriginalSenderNodeID)
		}

		// Note: This function does not check whether included ProblemHash is actually a currently valid block problem. This check will be done by the RA when it handles the received data.

		// verify that claimed SolutionHash was calculated correctly
		rebuiltBlockProblemSolution := simsol.NewBlockProblemSolution(blockProblemSolution.Subsolutions, blockProblemSolution.ProblemHash)
		if rebuiltBlockProblemSolution.SolutionHash.GetString() != blockProblemSolution.SolutionHash.GetString() {
			return nil, "", fmt.Errorf("TransportStructExtraction - Received solution claims to have solution hash %v but it actually has solution hash %v. Declining data sent by node %v \n", blockProblemSolution.SolutionHash.GetString(), rebuiltBlockProblemSolution.SolutionHash.GetString(), ts.OriginalSenderNodeID)
		}
		return ts.Data, rebuiltBlockProblemSolution.SolutionHash.GetString(), nil
	case TSData_MinerCommitment:
		var minerCommitment simsol.MinerCommitment
		err := msgpack.Unmarshal(ts.Data, &minerCommitment)
		if err != nil {
			return nil, "", fmt.Errorf("TransportStructExtraction - Failed to deserialize data into simsol.MinerCommitment due to error: %v. Declining data sent by node %v \n", err, ts.OriginalSenderNodeID)
		}
		return ts.Data, "abc", nil // i put bogus hash because its not used anyways in this case, when calling this function just _ the second returned value
	case TSData_RAcommitSecret:
		commitSecret := hex.EncodeToString(ts.Data)
		if len(commitSecret) != 142 { // 71 bytes expected
			return nil, "", fmt.Errorf("TransportStructExtraction - Failed to handle case RAcommitSecret because expected length of 142 but got length %v. Declining data sent by node %v \n", len(commitSecret), ts.OriginalSenderNodeID)
		}
		return ts.Data, commitSecret, nil // i put bogus hash because its not used anyways in this case
	case TSData_ChainDBRequest:
		var chaindbReq ChainDBRequest
		err := msgpack.Unmarshal(ts.Data, &chaindbReq)
		if err != nil {
			return nil, "", fmt.Errorf("TransportStructExtraction - Failed to deserialize data into ChainDBRequest due to error: %v. Declining data sent by node %v \n", err, ts.OriginalSenderNodeID)
		}
		return ts.Data, "abc", nil // i put bogus hash because its not used anyways in this case
	case TSData_Transaction:
		var recTransaction transaction.Transaction
		err := msgpack.Unmarshal(ts.Data, &recTransaction)
		if err != nil {
			return nil, "", fmt.Errorf("TransportStructExtraction - Failed to deserialize data into Transaction due to error: %v. Declining data sent by node %v \n", err, ts.OriginalSenderNodeID)
		}

		// check validity of transaction
		err, _, _, _, _ = StateDbTransactionIsAllowed(recTransaction)
		if err != nil {
			return nil, "", fmt.Errorf("TransportStructExtraction - Received transaction is invalid: %v. Declining data sent by node %v \n", err, ts.OriginalSenderNodeID)
		}
		return ts.Data, "abc", nil // i put bogus hash because its not used anyways in this case
	case TSData_LiveData:
		var recLiveData LiveData
		err := msgpack.Unmarshal(ts.Data, &recLiveData)
		if err != nil {
			return nil, "", fmt.Errorf("TransportStructExtraction - Failed to deserialize data into LiveData due to error: %v. Declining this live data..\n", err)
		}
		return ts.Data, "abc", nil // i put bogus hash because its not used anyways in this case
	default:
		// id doesnt exist [yet?]
		return nil, "", fmt.Errorf("TransportStructExtraction - DataID handling has not been implemented for this ID: %v. Declining data from orignal sender node %v \n", foundDataID, ts.OriginalSenderNodeID)
	}

}
