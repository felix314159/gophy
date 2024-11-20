void runSim( 
					UInt_t randomSeed = 1234,
				   	Int_t nEvents = 10,
				   	Int_t runID = 1337,
				   	Int_t chosenPart = 0,		            // not used, but kept here for compatibility with existing code
				   	Double_t momentum = 2.,                 // beam energy
				   	Double_t theta = 0.,
				   	const char* subProblemHash = "abcdef",
                   	TString mcEngine = "TGeant4",           // use geant4
                   	Bool_t isMT = true,
                   	Bool_t loadPostInitConfig = false)
{

    // output resulting files in same folder as this script
    TString outDir = "./";
    TString outFile = Form("%s/mc_%s.root?reproducible=mc", outDir.Data(), subProblemHash);
    TString geoFile = Form("geofile_%s.root?reproducible=geofile", subProblemHash);

    // configure simulation
    gDebug = 0;
    TStopwatch timer;
    timer.Start();

    FairRunSim* run = new FairRunSim();
    run->SetRunId(runID);   // use passed runID (line 102 of https://fairrootgroup.github.io/FairRoot/html/df/d06/FairRun_8h_source.html)
    run->SetName(mcEngine);
    FairGenericVMCConfig* config = new FairGenericVMCConfig();
    if (loadPostInitConfig)
        config->UsePostInitConfig();
    run->SetSimulationConfig(config);
    run->SetIsMT(isMT);     // multithreading is enabled
    run->SetSink(new FairRootFileSink(outFile));
    FairRuntimeDb* rtdb = run->GetRuntimeDb();

    // use some fair tutorial geometries as PoC
    run->SetMaterials("media.geo");

    FairModule* cave = new FairCave("CAVE");
    cave->SetGeometryFileName("cave_vacuum.geo");
    run->AddModule(cave);

    FairDetector* tutdet = new FairTutorialDet1("TUTDET", kTRUE);
    tutdet->SetGeometryFileName("double_sector.geo");
    run->AddModule(tutdet);

    FairPrimaryGenerator* primGen = new FairPrimaryGenerator();
    // set particle type that is generated (here 2212=proton, but this should in the future be passed as parameter to the script for more flexibility)
    FairBoxGenerator* boxGen = new FairBoxGenerator(2212, 1); // first parameter is PDG encoding of particle type (2212 = proton), 1 = multiplicity (https://fairrootgroup.github.io/FairRoot/html/db/dfe/FairBoxGenerator_8cxx_source.html line 69)
    // in-depth publication about the MC PDG codes at https://pdg.lbl.gov/2024/mcdata/mc_particle_id_contents.html , or just download the database that contains all the codes at https://pdg.lbl.gov/2024/api/index.html and in it check table 'pdgdata'
    // e.g fluka uses its own mapping to the existing pdg numbers: http://www.fluka.org/content/manuals/online/5.1.html
    // PDG Code Examples:
    //      11      = Electron
    //      2212    = Proton
    //      22      = Photon
    //      2112    = Neutron
    boxGen->SetThetaRange(0., theta);                        // set passed theta range
    boxGen->SetPRange(0., momentum);                         // set passed momentum range
    boxGen->SetPhiRange(0., 360.);                           // set phi range (currently not passed)
    // as shown in https://fairrootgroup.github.io/FairRoot/html/d1/dbd/FairBoxGenerator_8h_source.html more parameters could be specified like e.g.:
    //  - SetPtRange
    //  - SetEkinRange
    //  - SetEtaRange
    //  - SetYRange

    primGen->AddGenerator(boxGen);
    run->SetGenerator(primGen);

    // use pythia6 as external decayer (https://fairroot.gsi.de/index.html%3Fq=node%252F57.html)
    run->SetPythiaDecayer(kTRUE);

    // set passed custom RNG seed
    TRandom3 random(randomSeed);
    gRandom = &random;

    // run simtask
    run->Init();
    run->Run(nEvents);
    run->CreateGeometryFile(geoFile);

    // measure execution time
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
