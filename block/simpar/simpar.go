// Package simpar stands for 'simulation parameters'. In this package it is defined what a block problem is, how it is constructed and how to interface with the HEP environment. Defining problems is the responsibility of the Root Authority and there both is a way to manually enter a problem via locally hosted website or it will automatically generate a problem so that transaction throughput does not suffer if no manually defined problems are queued. RA only: In order to manually define a new simulation task navigate your browser to localhost:8087/send-simtask after you have found at least one peer.
package simpar

import (
	"fmt"
	"os/exec"
	"sync"
	"time"

	"github.com/vmihailenco/msgpack/v5"

	"example.org/gophy/block/hash"
	"example.org/gophy/block/simsol"
	"example.org/gophy/logger"
)

// Workflow:
//		1. Define all subproblems (instances of SimulationParameters)
//		2. Create []SimulationParameters
//		3. Create SimulationHeader by passing header data and []SimulationParameters to NewSimulationHeader
//		4. Create SimulationTask by passing []SimulationParameters and SimulationHeader to NewSimulationTask.
//		5. Broadcast SimulationTask to the node network (topic: pouw_newProblem) and collect solutions etc.

// SimulationParameters holds the parameters that define the simulation.
type SimulationParameters struct {
	Seed			uint32 		// Pythia max seed: 900000000 (negative would be default seed, so only positive here)
	AmountEvents	uint32
	RunID 			uint64
	Particles		uint32 		// 0=pions, 1=eplus, 2=proton
	Momentum 		float64
	Theta 			float64
}

// NewSimulationParameters is the constructor function for SimulationParameters.
func NewSimulationParameters(seed uint32, amountEvents uint32, runID uint64, particles uint32, momentum float64, theta float64) SimulationParameters {
	return SimulationParameters {
		Seed: 			seed,
		AmountEvents: 	amountEvents,
		RunID: 			runID,
		Particles: 		particles,
		Momentum: 		momentum,
		Theta: 			theta,
	}
}

// SimulationHeader holds the metadata of a problem definition.
// It has values such as CreationTime, ExpirationTime, BlockID, AmountSubProblems and SubProblemHashes.
type SimulationHeader struct {
	// metadata
	CreationTime		uint64		// when was this first broadcast
	ExpirationTime		uint64		// until when does problem solution have to be uploaded
	BlockID				uint32		// current blockID + 1 (human-readable value so that it's clear on which blocks this problem builds)
	AmountSubProblems 	uint32  	// amount of sub problems that need to be solved by the miners (length of SimulationParameters slice)
	RAcommit      		hash.Hash   // Keccak256(PrevBlockHash + secretBytes), which later is used in winner selection algorithm
	SubProblemHashes	[]hash.Hash // hash of [ serialized(SimulationParameters) + serialized(SimulationHeader) ]
}

// NewSimulationHeader is the constructor function for SimulationHeader. In addition to header related values it also requires the full slice of problem definitions.
func NewSimulationHeader(creationTime uint64, expirationTime uint64, blockID uint32, amountSubProblems uint32, raCommit hash.Hash, simParSlice []SimulationParameters ) SimulationHeader {
	// calculate hash of each subproblem to create slice that holds all of these hashes
	var subProblemHashSlice []hash.Hash
	for _, p := range simParSlice {
		// serialize current subproblem
		pSer, err := msgpack.Marshal(&p)
		if err != nil {
			logger.L.Panic(err)
		}
		// get its hash and append it to the slice
		pHash := hash.NewHash(string(pSer))
		subProblemHashSlice = append(subProblemHashSlice, pHash)
	}

	return SimulationHeader {
		CreationTime:		creationTime,
		ExpirationTime:		expirationTime,
		BlockID:			blockID,
		AmountSubProblems:	amountSubProblems,
		RAcommit:  			raCommit,
		SubProblemHashes:   subProblemHashSlice,
		
	}
}

// SimulationTask is equal to a full problem definition. It contains all required information (SimulationParameters and SimulationHeader).
type SimulationTask struct {
	// problem definitions (contains all subproblems)
	SimPar				[]SimulationParameters

	// metadata of this block problem
	SimHeader			SimulationHeader

	ProblemHash  		hash.Hash // hash of serialized SimHeader (this is the unique ProblemID)
}

// NewSimulationTask is the constructor function for SimulationTask.
// It takes a slice of problem definitions and a header and returns the hash of the block problem along with the problem itself.
func NewSimulationTask(simPar []SimulationParameters, simHeader SimulationHeader) SimulationTask {
	simTask := SimulationTask {
		SimPar:			simPar,
		SimHeader:		simHeader,
		ProblemHash: 	hash.Hash{}, // will be filled next
	}

	// get hash of simTask (unique block problem identifier)
	simTask.ProblemHash = simTask.GetHash()

	return simTask

}

// GetHash is a helper method of SimulationTask that returns the unique problem hash by serializing and hashing the block problem header.
func (s SimulationTask) GetHash() hash.Hash {
	//		serialize the header
	sHeaderSer, err := msgpack.Marshal(&s.SimHeader)
	if err != nil {
		logger.L.Panic(err)
	}
	//		get hash
	sHash := hash.NewHash(string(sHeaderSer))

	return sHash
}

// PrintSimTask is a method of SimulationTask that visually prints relevant info of the entire block problem.
func (s SimulationTask) PrintSimTask() {
	// print header metadata
    logger.L.Printf("Block problem header:\n\tUnique Hash: %v\n\tCreationTime: %v\n\tExpirationTime: %v\n\tBlockID: %v\n\tAmountSubProblems: %v\n\tRAcommit: %v\n\n", s.ProblemHash.GetString(), s.SimHeader.CreationTime, s.SimHeader.ExpirationTime, s.SimHeader.BlockID, s.SimHeader.AmountSubProblems, s.SimHeader.RAcommit.GetString())

	// print information about each subproblem
	for subIndex, subProblem := range s.SimPar {
		logger.L.Printf("Subproblem #%v:\n\tHash: %v\n\tSeed: %v\n\tAmountEvents: %v\n\tRunID: %v\n\tParticles: %v\n\tMomentum: %v\n\tTheta: %v\n", subIndex, s.SimHeader.SubProblemHashes[subIndex].GetString(), subProblem.Seed, subProblem.AmountEvents, subProblem.RunID, subProblem.Particles, subProblem.Momentum, subProblem.Theta)
	}
}

// ---- Run simulation ----

// RunSimMutex is a mutex used to prevent a miner from working on old and new block problem at the same time (e.g. while working on old problem a new one is received and this function triggered again)
var RunSimMutex sync.Mutex

// RunSimulation takes a SimulationParameters object, runs the simulation and returns the serialized block problem solution (which contains all subproblem solutions) and an error.
// The simulation can only be run if the code is run in the CBM environment (and my code has only been tested on Ubuntu CBM).
// Note: In order to be able to use root in cbm env run 'nano ~/.profile', then add the following lines if they don't exist already:
//          export PATH="$HOME/fairsoft_apr21p2_root6/installation/bin:$PATH"
//			source "$HOME/fairroot_v18.6.7-fairsoft_apr21p2_root6/bin/FairRootConfig.sh"
//			export PATH=$PATH:/usr/local/go/bin
//       then reboot. Typing 'root' in bash should now work.
func (s SimulationTask) RunSimulation() ([]byte, hash.Hash, error) {
	RunSimMutex.Lock()
	defer RunSimMutex.Unlock()

	// store subproblem solutions
	subProblemSolutionSlice := []simsol.SimulationSolution{}

	logger.L.Printf("Received new block problem with hash '%v'.\nStarting to run simulations..\n", s.ProblemHash.GetString())

	// each sub-problem already makes use of geant4 multithreading, so a sequential call is ok for now (if you have lots of cores you can adjust some code to solve each subproblem in parallel)
	for pIndex, p := range s.SimPar {
		// get hash of current subproblem (will be part of the output filename, e.g. mc_<hash>.root)
		subProblemHashString := s.SimHeader.SubProblemHashes[pIndex].GetString()

		// generate sim command, e.g. root -l -q -b 'runSim.C(123, 14, 100, 0, 2., 0.)'
		// 		uses 4 fractional digits for floats and ignores the rest!
		simCommand := "root -l -q -b 'runSim.C(" + fmt.Sprint(p.Seed) + ", " + fmt.Sprint(p.AmountEvents) + ", " + fmt.Sprint(p.RunID) + ", " + fmt.Sprint(p.Particles) + ", " + fmt.Sprintf("%.4f", p.Momentum) + ", " + fmt.Sprintf("%.4f", p.Theta) + ", \"" + subProblemHashString + "\"" + ")'"
		logger.L.Printf("%v: Will now try to solve subproblem #%v/%v (subproblem hash: %v) by running command:\n\t%v\n", GetTime(), pIndex, s.SimHeader.AmountSubProblems-1, subProblemHashString, simCommand)
		
		// try to run simulation
		cmd := exec.Command("bash", "-c", "cd ./simdata && " + simCommand + " >> /dev/null 2>&1")
		_, err := cmd.CombinedOutput()
		if err != nil {
			logger.L.Printf("Error: %v", err)
			return nil, hash.Hash{}, fmt.Errorf("RunSimulation - Failed to run simulation command: %v\n", err)
		}
		logger.L.Printf("%v: Successfully solved problem #%v/%v\n---\n", GetTime(), pIndex, s.SimHeader.AmountSubProblems-1)

		// read solution from file system (no need to use filepath here as simulations only are supposed to run under linux or macOS anyways)
		//		get path of geofile
		geoFilePath := fmt.Sprintf("./simdata/geofile_%v.root", subProblemHashString)
		//		get path of mcfile
		mcFilePath := fmt.Sprintf("./simdata/mc_%v.root", subProblemHashString)
		//		initialize filepath object instance
		subProblemSimpath := simsol.NewSimulationPath(geoFilePath, mcFilePath)
		//		read data from file system
		subProblemSimSol, err := simsol.NewSimulationSolution(subProblemSimpath)
		if err != nil {
			logger.L.Panic(err)
		}

		// append this subproblem solution to the slice
		subProblemSolutionSlice = append(subProblemSolutionSlice, subProblemSimSol)

	}

	// create BlockProblemSolution instance (holds all entire solution + unique problem ID)
	blockProblemSolution := simsol.NewBlockProblemSolution(subProblemSolutionSlice, s.ProblemHash)

	// remember your solution hash (it is later used to determine the committment you broadcast to everyone)
	solutionHash := blockProblemSolution.SolutionHash

	// get serialized block problem solution
	blockProblemSolutionSer, err := msgpack.Marshal(&blockProblemSolution)
	if err != nil {
		logger.L.Panic(err)
	}

	return blockProblemSolutionSer, solutionHash, nil
}

// GetTime returns the current time as human-readable string.
func GetTime() string {
	currentTime := time.Now()
	return fmt.Sprintf("\n%02d-%02d-%02d %02d:%02d:%02d", currentTime.Year(), currentTime.Month(), 
											  currentTime.Day(),  currentTime.Hour(), 
											  currentTime.Minute(), currentTime.Second() )
}