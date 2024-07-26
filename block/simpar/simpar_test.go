package simpar

import (
	"testing"

	"example.org/gophy/block/hash"
)

func TestSimpar(t *testing.T) {

	// define simparameters
	simParametersSlice := []SimulationParameters{}
	p := NewSimulationParameters(uint32(555), uint32(2), uint64(15), uint32(0), float64(1.01), float64(0.))
	simParametersSlice = append(simParametersSlice, p)

	// define simheader
	h := NewSimulationHeader(0, 0, uint32(0), uint32(0), hash.Hash{}, []SimulationParameters{})

	// create simtask
	myTask := NewSimulationTask(simParametersSlice, h)

	// compare hash of simtask against known correct value for this example
	expectedHash := "c5516fb650315116d18be0454f46e843ee417db2141f3006576681dc92e62542"
	if myTask.GetHash().GetString() != expectedHash {
		t.Errorf("simpar failed test: SimulationTask has unexpected hash: %v. Expected: %v", myTask.GetHash().GetString(), expectedHash)
	}

}
