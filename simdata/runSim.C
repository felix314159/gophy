// How to run example:
// 		root -l -q -b 'runSim.C(123, 14, 100, 0, 2., 0., "abcdef")'
// This allows passing parameters e.g. randomSeed 123, nEvents 14, runID 100, chosenPart 0 (pions), momentum 2., theta 0 and subproblem hash abcdef.

void runSim( // default values that can be overwritten by passing arguments like shown above
					UInt_t randomSeed = 1234,
				   	Int_t nEvents = 10,
				   	Int_t runID = 1337,
				   	Int_t chosenPart = 0,		// 0=pions, 1=eplus, 2=proton
				   	Double_t momentum = 2.,
				   	Double_t theta = 0.,
				   	const char* subProblemHash = "abcdef", // must be const, otherwise you get "ISO C++11 does not allow conversion from string literal to 'char *'" warnings
                   	TString mcEngine = "TGeant4", // you could also use TGeant3
                   	Bool_t isMT = true,
                   	Bool_t loadPostInitConfig = false)
{

    TString dir = getenv("VMCWORKDIR");
    TString tutdir = dir + "/simulation/Tutorial1";

    TString tut_geomdir = dir + "/common/geometry";
    gSystem->Setenv("GEOMPATH", tut_geomdir.Data());

    TString tut_configdir = dir + "/common/gconfig";
    gSystem->Setenv("CONFIG_DIR", tut_configdir.Data());

    TString partName[] = {"pions", "eplus", "proton"};
    Int_t partPdgC[] = {211, 11, 2212};
    
    TString outDir = "./";

    // you must use ?reproducible=<name> to have a reproducible hash (note that changing <name> also affects hash!)
    TString outFile = Form("%s/mc_%s.root?reproducible=mc", outDir.Data(), subProblemHash);

	// you must use ?reproducible=<name>
    TString geoFile = Form("geofile_%s.root?reproducible=geofile", subProblemHash);

    // Configure Simulation
    // ========================================================================

    // ----    Debug option   -------------------------------------------------
    gDebug = 0;
    // ------------------------------------------------------------------------

    // -----   Timer   --------------------------------------------------------
    TStopwatch timer;
    timer.Start();
    // ------------------------------------------------------------------------

    // -----   Create simulation run   ----------------------------------------
    FairRunSim* run = new FairRunSim();
    // you need to set the same run ID if you want reproducible hash! line below makes tutorial1_TGeant3_pions.mc.. file hash consistent
    run->SetRunId(runID);
    run->SetName(mcEngine);   // Transport engine
    FairGenericVMCConfig* config = new FairGenericVMCConfig();
    if (loadPostInitConfig)
        config->UsePostInitConfig();
    run->SetSimulationConfig(config);
    run->SetIsMT(isMT);                            // Multi-threading mode (Geant4 only)
    run->SetSink(new FairRootFileSink(outFile));   // Output file
    FairRuntimeDb* rtdb = run->GetRuntimeDb();
    // ------------------------------------------------------------------------

    // -----   Create media   -------------------------------------------------
    run->SetMaterials("media.geo");   // Materials
    // ------------------------------------------------------------------------

    // -----   Create geometry   ----------------------------------------------

    FairModule* cave = new FairCave("CAVE");
    cave->SetGeometryFileName("cave_vacuum.geo");
    run->AddModule(cave);

    FairDetector* tutdet = new FairTutorialDet1("TUTDET", kTRUE);
    tutdet->SetGeometryFileName("double_sector.geo");
    run->AddModule(tutdet);
    // ------------------------------------------------------------------------

    // -----   Create PrimaryGenerator   --------------------------------------
    FairPrimaryGenerator* primGen = new FairPrimaryGenerator();
    FairBoxGenerator* boxGen = new FairBoxGenerator(partPdgC[chosenPart], 1);

    boxGen->SetThetaRange(theta, theta + 0.01);
    boxGen->SetPRange(momentum, momentum + 0.01);
    boxGen->SetPhiRange(0., 360.);
    boxGen->SetDebug(kTRUE);

    primGen->AddGenerator(boxGen);

    run->SetGenerator(primGen);
    // ------------------------------------------------------------------------

    // -----   Initialize simulation run   ------------------------------------
    TRandom3 random(randomSeed);
    gRandom = &random;

    run->Init();
    // ------------------------------------------------------------------------

    // -----   Start run   ----------------------------------------------------
    run->Run(nEvents);
    run->CreateGeometryFile(geoFile);
    // ------------------------------------------------------------------------

    // -----   Measure execution time   ---------------------------------------

    cout << endl << endl;

    FairSystemInfo sysInfo;
    Float_t maxMemory = sysInfo.GetMaxMemory();
    cout << "<DartMeasurement name=\"MaxMemory\" type=\"numeric/double\">";
    cout << maxMemory;
    cout << "</DartMeasurement>" << endl;

    timer.Stop();
    Double_t rtime = timer.RealTime();
    Double_t ctime = timer.CpuTime();

    Float_t cpuUsage = ctime / rtime;
    cout << "<DartMeasurement name=\"CpuLoad\" type=\"numeric/double\">";
    cout << cpuUsage;
    cout << "</DartMeasurement>" << endl;

    cout << endl << endl;
    cout << "Output file is " << outFile << endl;
    cout << "Real time " << rtime << " s, CPU time " << ctime << "s" << endl << endl;
    cout << "Macro finished successfully." << endl;

}

