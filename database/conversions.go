package database

// conversions.go takes care of conversions between various data types and structs

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/btcsuite/btcutil/base58"
	"github.com/libp2p/go-libp2p/core/crypto"
	"github.com/libp2p/go-libp2p/core/peer" // pubkey -> readable nodeID string

	"github.com/vmihailenco/msgpack/v5"

	"github.com/felix314159/gophy/block"
	"github.com/felix314159/gophy/block/hash"
	"github.com/felix314159/gophy/block/simpar"
	"github.com/felix314159/gophy/block/simsol"
	"github.com/felix314159/gophy/block/transaction"
	"github.com/felix314159/gophy/logger"
)

// BytesToStringSlice unmarshals []byte to []string. It is used during networking to prepare to request each required block one-by-one.
func BytesToStringSlice(acceptedData []byte) []string {
	var acceptedDataStringSlice []string
	err := msgpack.Unmarshal(acceptedData, &acceptedDataStringSlice)
	if err != nil {
		logger.L.Panic(err) // if majority of nodes sent data that is invalid there is no point in communicating further
	}
	return acceptedDataStringSlice
}

// FullBlockToFullBlockBytes takes a block instance and serializes it and returns these serialized block bytes.
func FullBlockToFullBlockBytes(blk block.Block) []byte {
	blkSer, err := msgpack.Marshal(&blk)
	if err != nil {
		logger.L.Panic(err)
	}

	return blkSer
}

// FullBlockBytesToBlock takes a serialized byte slice that represents a full block and returns a block.Block object.
// This function is used e.g. in combination with BlockGetBytesFromDb() which returns a serialized block from the db.
func FullBlockBytesToBlock(blockData []byte) block.Block {
	// deserialize the block
	var blockNew block.Block	// dont call this variable block or you will be shadowing package block
	err := msgpack.Unmarshal(blockData, &blockNew)
	if err != nil {
		logger.L.Panic(err)
	}

	return blockNew

}

// FullBlockBytesToBlockLight takes a serialized byte slice that represents a block.Block and returns a block.Header (which basically is a light block) and its hash.
// It's used so that full nodes can serve content to light nodes.
func FullBlockBytesToBlockLight(blockData []byte) (block.Header, hash.Hash) {
	// deserialize the block
	var blockFull block.Block	// dont call this variable block or you will be shadowing package block
	err := msgpack.Unmarshal(blockData, &blockFull)
	if err != nil {
		logger.L.Panic(err)
	}
	blockHeader := blockFull.BlockHeader

	// restore hash
	blockHeaderSer, err := msgpack.Marshal(&blockHeader)
	if err != nil {
		logger.L.Panic(err)
	}
	
	return blockHeader, hash.NewHash(string(blockHeaderSer))

}

// HeaderBytesToBlockLight takes a serialized byte slice that represents a block.Header and returns a block.Header.
func HeaderBytesToBlockLight(blockData []byte) (block.Header, hash.Hash) {
	// deserialize the block
	var headerFull block.Header	// dont call this variable block or you will be shadowing package block
	err := msgpack.Unmarshal(blockData, &headerFull)
	if err != nil {
		logger.L.Panic(err)
	}

	// restore hash
	headerFullSer, err := msgpack.Marshal(&headerFull)
	if err != nil {
		logger.L.Panic(err)
	}
	
	return headerFull, hash.NewHash(string(headerFullSer))

}

// HeaderToBytes takes a block.Header and returns it as msgpacked []byte.
func HeaderToBytes(h block.Header) []byte {
	// serialize
	headerSer, err := msgpack.Marshal(&h)
	if err != nil {
		logger.L.Panic(err)
	}

	return headerSer
}

// HeaderToHash takes a block.Header and returns its hash.Hash
func HeaderToHash(header block.Header) hash.Hash {
	// serialize header
	headerSer, err := msgpack.Marshal(&header)
	if err != nil {
		logger.L.Panic(err)
	}
	
	// get hash of serialized header
	headerHash := hash.NewHash(string(headerSer))
	
	return headerHash
}

// UnpackSerializedStringSliceBlockHashes takes msgpack serialized slice of strings, then unpacks and returns it
func UnpackSerializedStringSliceBlockHashes(receivedMessage []byte) ([]string, error) {
	var stringSliceBlockHashes []string
	err := msgpack.Unmarshal(receivedMessage, &stringSliceBlockHashes)
	if err != nil {
		return nil, fmt.Errorf("UnpackSerializedStringSliceBlockHashes - Failed to unmarshal the received blockHash data: %v", err)
	} 
	if len(stringSliceBlockHashes) < 1 {
		return nil, fmt.Errorf("UnpackSerializedStringSliceBlockHashes - Unpacked an empty string slice!")
	}

	return stringSliceBlockHashes, nil
}

// hexStringToUint32 takes a hash hex string as input and returns a custom uint32 representation of its first 4 chars.
// This is used by the RA to determine the seed of the next problem definition depending on the hash of the latest block.
// The uint32 result can be converted back to the the first 4 digits of the original string using uint32ToHexString, however the casing information is lost.
// Largest possible result is 70707070, smallest possible result is 48484848
func hexStringToUint32(hexString string) (uint32, error) {
	hexString = strings.ToUpper(hexString) // upper is better than lower to guarantee ascii dec is 2 digits (otherwise d, e, f, .. would be triple digits)

	// ensure the length is equal to 64 (keccak256 produces output of 256 bits = 32 bytes = 64 hex chars)
	if len(hexString) != 64 {
		return 0, fmt.Errorf("hexStringToUint32 - Expected string of exactly length 64 hex chars but got %v which is of length %v\n", hexString, len(hexString))
	}

	// ensure this is a hex string
	for _, c := range hexString {
		cInt := int(c)
		validChar := (cInt < 58 && cInt > 47) || (cInt > 64 && cInt < 71) // ensure it is either 0-9 or A-F (im not good at regex so i think this is more readable)
		if !validChar {
			return 0, fmt.Errorf("hexStringToUint32 - Invalid char in hex string detected. String %v contains %v which is invalid\n", hexString, c)
		}
	}

	// now for the first 4 chars, each char is appended to the result as string. largest possible number: 70 70 70 70 (FFFF) which is smaller than Pythia max seed 900000000, smallest possible number: 48 48 48 48. it is impossible that an invalid number is formed or that the resulting string length is not 8
	resultString := ""
	for i := 0; i < 4; i++ {
		resultString += fmt.Sprintf("%v", int(hexString[i]))
	}

	// convert to uint64
	value, err := strconv.ParseUint(resultString, 10, 32) // parse it as base 10 (decimal) and unsigned 32-bits
	if err != nil {
		return 0, err
	}

	// return as uint32
	return uint32(value), nil
}

// uint32ToHexString takes a result generated by running hexStringToUint32 as input, and returns the first 4 chars (casing information is lost though) of the string that originally was used to create the input value and returns them as string
func uint32ToHexString(inputUint uint32) string {
	inputString := fmt.Sprintf("%v", inputUint)
	if len(inputString) != 8 {
		logger.L.Panicf("uint32ToHexString - Invalid input length. Expected number with 8 digits as input but got %v which has %v digits.\n", inputString, len(inputString))
	}

	result := ""
	for i:=0; i<8; i+=2 {
		byteRecon := inputString[i:i+2] // combine two hex chars into a string which will be interpreted as ascii dec value
		result += asciiDecStringToChar(byteRecon)
	}

	return result
}

// asciiDecStringToChar takes an ascii dec string and returns the representated ascii char as string
func asciiDecStringToChar(input string) string {
	asciiValue, err := strconv.Atoi(input) // string to int
	if err != nil {
		logger.L.Panicf("asciiDecStringToChar - %v", err)
	}

	return string(rune(asciiValue)) // convert int to a rune, then to a string
}

// SimtaskBytesToSimTask takes serialized SimulationTask as bytes and returns it as simpar.SimulationTask object.
// Only call this after TSStructExtraction verified that it is valid data that actually can be deserialized without issues.
func SimtaskBytesToSimTask(s []byte) simpar.SimulationTask {
	var simTaskObject simpar.SimulationTask
	err := msgpack.Unmarshal(s, &simTaskObject)
	if err != nil {
		logger.L.Panic(err)
	}

	return simTaskObject
}

// SimtaskToBytes takes a SimulationTask and returns the msgpacked bytes.
func SimtaskToBytes(s simpar.SimulationTask) []byte {
	simTaskSer, err := msgpack.Marshal(&s)
	if err != nil {
		logger.L.Panic(err)
	}

	return simTaskSer
}

// SimSolToBytes takes a simsol.SimulationSolution, serializes it and returns that data and an error.
func SimSolToBytes(s simsol.SimulationSolution) ([]byte, error) {
	simsolSer, err := msgpack.Marshal(&s)
	if err != nil {
		return nil, fmt.Errorf("SimSolToBytes - Failed to serialize SimulationSolution due to error: %v\n", err)
	}

	return simsolSer, nil
}

// BytesToSimsolBlockProblemSolution takes bytes and deserializes them into simsol.BlockProblemSolution.
// This function is used only by the RA after it has received a miner solution that it knows it can deserialize.
// Only call this after TSStructExtraction verified that it is valid data that actually can be deserialized without issues.
func BytesToSimsolBlockProblemSolution(s []byte) simsol.BlockProblemSolution {
	var recSimSol simsol.BlockProblemSolution

	err := msgpack.Unmarshal(s, &recSimSol)
	if err != nil {
		logger.L.Panic(err)
	}

	return recSimSol
}

// StateDbBytesToStruct takes bytes that were retrieved from the statedb and returns the corresponding StateValueStruct instance and an error.
func StateDbBytesToStruct(ser []byte) (StateValueStruct, error) {
	var deser StateValueStruct
	err := msgpack.Unmarshal(ser, &deser)
	if err != nil {
		return StateValueStruct{}, fmt.Errorf("StateDbBytesToStruct - Failed to deserialize bytes to StateValueStruct due to error: %v\n", err)
	}

	return deser, nil
}

// StateDbStructToBytes takes a StateValueStruct and msgpack serializes it. Returns serialized data and error.
func StateDbStructToBytes(s StateValueStruct) ([]byte, error) {
	stateValueStructSer, err := msgpack.Marshal(&s)
	if err != nil {
		return nil, fmt.Errorf("StateDbStructToBytes - Failed to serialize StateValueStruct due to error: %v\n", err)
	}

	return stateValueStructSer, nil
}

// HexStringToNumber takes the output string of a hash function and returns a uint64 number that is derived from this value.
// The first 15 chars of the input string are converted (to guarantee that it fits into uint64) and the rest is ignored.
func HexStringToNumber(a string) uint64 {
	if len(a) < 15 {
		logger.L.Panicf("Input string is definitely not the output of a hash function, got: %v", a)
	}

	// take first 15 chars of a and convert to decimal number
	aNumber, err := strconv.ParseInt(a[:15], 16, 64)
	if err != nil {
		logger.L.Panic(err) 
	}
	return uint64(aNumber) // allowed and safe because a is output of keccak256 which can't contain '-' which would result in negative number when ParseInt'ed
}

// ---- Node ID and PubKey conversions ----

// NodeIDStringToPubKey uses btcsuite's base58 decode to convert NodeID to the Ed25519 public key of that node.
// This function is required so that signatures can be verified. Alternatively, you can use libp2p builtin function to get public key from node id.
func NodeIDStringToPubKey(nodeIDString string) (crypto.PubKey, error) {
	// decode base58 format the provided node ID is in
	nodeIDRawBytes := base58.Decode(nodeIDString)[6:]

	// derive Ed25519 public key from the extracted bytes
	publicKeySender, err := crypto.UnmarshalEd25519PublicKey(nodeIDRawBytes)
	if err != nil {
		return nil, fmt.Errorf("NodeIDStringToPubKey - Failed to create Ed25519 public key from given Node ID: %v", err)
	} 
	
	// ensure that the key is valid and you can use functions like Raw() on it
	_, err = publicKeySender.Raw()
	if err != nil {
		return nil, fmt.Errorf("NodeIDStringToPubKey - Failed to extract raw bytes from public key which means that the pubkey itself is invalid: %v", err)
	}
	
	// logger.L.Printf("Public key: %v", hex.EncodeToString(pubkeyBytes))

	return publicKeySender, nil
	
}

// PubKeyToNodeID takes a PubKey and returns the human-readable node ID (12D3Koo...)
func PubKeyToNodeID(pubKeyObject crypto.PubKey) (string, error) {
	peerID, err := peer.IDFromPublicKey(pubKeyObject)
	if err != nil {
		return "", fmt.Errorf("Failed to convert PubKey to peerID: %v", err)
	}

	return peerID.String(), nil
}

// ----

// ChainDBRequestToBytes casts an instance of ChainDBRequest to bytes by serializing it.
func ChainDBRequestToBytes(c ChainDBRequest) ([]byte, error) {
	cSer, err := msgpack.Marshal(&c)
	if err != nil {
		return nil, fmt.Errorf("ChainDBRequestToBytes - Failed to serialize ChainDBRequest due to error: %v\n", err)
	}

	return cSer, nil
}

// BytesToChainDBRequest unmarshals a serialized ChainDBRequest if possible.
func BytesToChainDBRequest(b []byte) (ChainDBRequest, error) {
	var bDeser ChainDBRequest
	err := msgpack.Unmarshal(b, &bDeser)
	if err != nil {
		return ChainDBRequest{}, fmt.Errorf("BytesToChainDBRequest - Failed to deserialize bytes to ChainDBRequest due to error: %v\n", err)
	}

	return bDeser, nil
}

// TransactionToBytes serializes a transaction and returns []byte and error.
func TransactionToBytes(t transaction.Transaction) ([]byte, error) {
	tSer, err := msgpack.Marshal(&t)
	if err != nil {
		return nil, fmt.Errorf("TransactionToBytes - Failed to serialize transaction due to error: %v\n", err)
	}

	return tSer, nil
}

// TransactionSliceToBytes serializes []transaction.Transaction and returns []byte and error.
func TransactionSliceToBytes(pt []transaction.Transaction) ([]byte, error) {
	ptSer, err := msgpack.Marshal(&pt)
	if err != nil {
		return nil, fmt.Errorf("TransactionSliceToBytes - Failed to serialize slice of transactions due to error: %v\n", err)
	}

	return ptSer, nil
}
