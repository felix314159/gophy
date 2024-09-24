// Package simsol stands for 'simulation solution' and it contains structs and functions to work with files created by running simulation tasks.
package simsol

import (
	"fmt"
	"os"
	"path/filepath" // paths that are compatible with different OS

	"github.com/vmihailenco/msgpack/v5"

	"github.com/felix314159/gophy/logger"
	"github.com/felix314159/gophy/block/hash"
)

// SimulationPath holds filepaths to the data that was created by running a simulation. Each subproblem solution has its own SimulationPath instance.
type SimulationPath struct {
	Geofile		string
	MCfile		string
}

// NewSimulationPath is the constructor function of SimulationPath.
func NewSimulationPath(geoFilePath string, mcFilePath string) SimulationPath {
	return SimulationPath{
		Geofile: geoFilePath,
		MCfile: mcFilePath,
	}
}

// SimulationSolution holds a subproblem solution. It stores the different files that are created during simulation as bytes.
type SimulationSolution struct {
	GeoBytes        []byte
	MCBytes			[]byte
}

// NewSimulationSolution is the constructor function of SimulationSolution.
// It reads simulation-related files from the file system, stores them in a SimulationSolution instance and returns all of this data along with an error.
func NewSimulationSolution(simPath SimulationPath) (SimulationSolution, error) {
	// read solution from file system
	geoData, mcData, err := simPath.SimsolReadBytes()
	if err != nil {
		return SimulationSolution{}, fmt.Errorf("NewSimulationSolution - Read file error: %v \n", err)
	}

	// create solution object
	simSolObject := SimulationSolution {
		GeoBytes:		geoData,
		MCBytes:       	mcData,
	}

	return simSolObject, nil
}


// BlockProblemSolution contains all subproblem solution, the problem ID and the solution hash. This is what nodes serializes and broadcast to RA after they have finished all simulations.
type BlockProblemSolution struct {
	Subsolutions 	[]SimulationSolution
	ProblemHash  	hash.Hash // unique block problem hash that represents the problem that was solved with this solution
	SolutionHash    hash.Hash // hash of this solution is determined by combining and hashing the fields Subsolutions and ProblemHash.
}

// NewBlockProblemSolution is the constructor of lockProblemSolution. It automatically determines the solution hash which is stored in the field SolutionHash.
func NewBlockProblemSolution(subSolSlice []SimulationSolution, problemHash hash.Hash) BlockProblemSolution {
	// get your solution hash 
	// 		1. serialize the slice that holds all subproblem solutions to get the block problem solution in serialized form
	subSolSliceSer, err := msgpack.Marshal(&subSolSlice)
	if err != nil {
		logger.L.Panic(err)
	}
	// 		2. hash the serialized block problem solution to determine your solution hash
	solH := hash.NewHash(string(subSolSliceSer))

	return BlockProblemSolution {
		Subsolutions: subSolSlice,
		ProblemHash: problemHash,
		SolutionHash: solH, // this is your solution hash for this block problem
	}

}

// MinerCommitment is a struct used after a miner solved the block problem to store original sender ID, Hash(Hash(solutiondata)) and Sig(solutionHash)
type MinerCommitment struct {
	OriginalSenderNodeID  	string 		// NodeID who this commitment is originally from. This is required because pubsub.Message.ReceivedFrom is the address of whoever forwarded the message to you, but this does not have to be the original sender!
	HashCommit				hash.Hash 	// Hash of hash (prove you know the hash without revealing it)
	SigCommit 				[]byte 		// Sig of hash (so that miners can not just re-broadcast the Hash-of-hashes of other miners' solutions)
	Timestamp 				uint64  	// Time at which the commitment was created (used to prevent replay attacks)
}


// ---- Methods ----

// SimsolReadBytes takes a SimulationPath and then reads the bytes of both files from the filesystem and returns them along with an error.
func (simPath SimulationPath) SimsolReadBytes() ([]byte, []byte, error) {
	// read geofile
	geofileData , err := os.ReadFile(simPath.Geofile)	// assumes that files can be held in memory (not a problem as long as simfiles in total are less than like 2 GB)
	if err != nil {
		return nil, nil, fmt.Errorf("SimsolReadBytes - Read file error: %v \n", err)
	}
	// read mcfile
	mcfileData , err := os.ReadFile(simPath.MCfile)
	if err != nil {
		return nil, nil, fmt.Errorf("SimsolReadBytes - Read file error: %v \n", err)
	}

	return geofileData, mcfileData, nil
}

// ---- Functions ----

// WriteSimsolbytesToFilesystem takes a serialized BlockProblemSolution and the blockID that the solution will be included as winner solution in, then deserializes the solution and then writes the sim files to disk.
func WriteSimsolbytesToFilesystem(serSolution []byte, blockID uint32) {
	// deserialize data
	var deserProblemSol BlockProblemSolution
	err := msgpack.Unmarshal(serSolution, &deserProblemSol)
	if err != nil {
		logger.L.Panic(err)
	}

	// ---- Define filepaths ----
	simdataPath := filepath.Join(".", "simdata")
	blockIDPath := filepath.Join(simdataPath, fmt.Sprint(blockID)) // use fmt.Sprint to cast uint32 to string, not string()
	solutionHashPath := filepath.Join(blockIDPath, deserProblemSol.SolutionHash.GetString())

	// ---- Create necessary folders if they do not exist already ----
	CreateFolder(simdataPath)
	CreateFolder(blockIDPath)
	CreateFolder(solutionHashPath)

	// ---- Iterate over subproblems and write sim files into their respective dirs
	for index, subsol := range deserProblemSol.Subsolutions {
		// define filepaths
		subProblemFolderName := "p" + fmt.Sprint(index+1)
		subsolFolderPath := filepath.Join(solutionHashPath, subProblemFolderName)
		//		sim files
		mcPath := filepath.Join(subsolFolderPath, "mc.root") 		// at some point maybe add more sim information to filename itself
		geoPath := filepath.Join(subsolFolderPath, "geofile.root") 	// at some point maybe add more sim information to filename itself

		// create folder for this subproblem (p1 - pn if there are n subproblems)
		CreateFolder(subsolFolderPath)

		// ---- Write simfiles to disk (if they exist already they will be overwritten [only relevant for testing]) ----

		// 1. geo file
		// 		Create the file
		geoFile, err := os.Create(geoPath)
		if err != nil {
			logger.L.Panic(err)
		}
		defer geoFile.Close()
		// 		Write to the file
		_, err = fmt.Fprintf(geoFile, "%s", subsol.GeoBytes)
		if err != nil {
			logger.L.Panic(err)
		}

		// 2. mc file
		// 		Create the file
		mcFile, err := os.Create(mcPath)
		if err != nil {
			logger.L.Panic(err)
		}
		defer mcFile.Close()
		// 		Write to the file
		_, err = fmt.Fprintf(mcFile, "%s", subsol.MCBytes)
		if err != nil {
			logger.L.Panic(err)
		}

	}

	logger.L.Printf("SimsolbytesToFilesystem - Successfully persisted simulation files on disk.")

}

// CreateFolder takes a path to a folder and creates that folder if it does not exist already.
func CreateFolder(folderPath string) {
	if _, err := os.Stat(folderPath); os.IsNotExist(err) {
		err := os.MkdirAll(folderPath, 0755)
		if err != nil {
			logger.L.Panic(err)
		}
	}
}

// DeleteFolder takes a path to a folder and deletes it (and all files it contains) if it does exist.
func DeleteFolder(folderPath string) {
	if _, err := os.Stat(folderPath); !os.IsNotExist(err) {
		// the directory exists, so delete it
		err := os.RemoveAll(folderPath)
		if err != nil {
			logger.L.Panic(err)
		}
	}
}
