package simsol

import (
	"testing"
	"path/filepath"
	//"os"		// read/write files to disk
)

func TestSimsol(t *testing.T) {
	rootFolderName := "gophy" // dir name of golang project rootfolder

	// define simulation path object
	simPath := SimulationPath {
		Geofile:	filepath.Join("..", "..", rootFolderName, "simdata", "geofile.root"),
		MCfile:		filepath.Join("..", "..", rootFolderName, "simdata", "mc.root"),
	}

	if len(simPath.Geofile) < 1 {} // just silence warning, this test works as long as simulationpath format is not changed



}
