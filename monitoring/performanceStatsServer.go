// Package monitoring contains code related to collecting and sending performance data of nodes to a locally hosted website at localhost:12345. It records certain event such as when a node connected to other nodes or completed its initial sync. I prefered writing my own lightweight monitoring that I fully understand over using sth like Grafana + Prometheus.
package monitoring

import (
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"sync"

	"example.org/gophy/logger"
)

const serverPort = 12345

var (
	messages []PerformanceData
	mu       sync.Mutex
	// monitoringWebsiteLocation holds the OS-agnostic path to the performanceStats.html file
	monitoringWebsiteLocation = filepath.Join(".", "monitoring", "performanceStats.html")
)

// StartPerformanceStatsServer is used to start the server that displays performance data submitted by nodes in the browser.
func StartPerformanceStatsServer() {
	http.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		// set content type
		w.Header().Set("Content-Type", "text/html")
		
		// read html file
		htmlContent, err := os.ReadFile(monitoringWebsiteLocation)
		if err != nil {
			logger.L.Panicf("StartPerformanceStatsServer - Failed to read performanceStats.html: %v", err)
		}

		// display website
		fmt.Fprintf(w, string(htmlContent))
	})

	// function to handle data received via /messages
	http.HandleFunc("/messages", func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		defer mu.Unlock()

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]interface{}{
			"messages": messages,
		})
	})

	// function to handle data received via /send
	http.HandleFunc("/send", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "Invalid request method", http.StatusMethodNotAllowed)
			return
		}

		// retrieve JSON field values
		timeRec := r.FormValue("time")
		sender := r.FormValue("sender")
		eventtype := r.FormValue("eventtype")
		message := r.FormValue("message")

		if timeRec == "" || sender == "" || eventtype == "" || message == "" {
			http.Error(w, "All message fields must be non-empty!", http.StatusBadRequest)
			return
		}

		mu.Lock()
		messages = append(messages, PerformanceData {
			Time:    	timeRec,
			Sender:  	sender,
			EventType: 	eventtype,
			Message: 	message,
		})
		mu.Unlock()

		w.WriteHeader(http.StatusOK)
		fmt.Fprintf(w, "ACK") // reply to client with ACK so that it knows the message was received correctly

	})

	// try to start running server
	logger.L.Printf("Starting performance stats server at localhost:%v\n", serverPort)
	if err := http.ListenAndServe(fmt.Sprintf(":%v", serverPort), nil); err != nil {
		// "panic: listen tcp :12345: bind: address already in use" happens when you try to run multiple instances on the same port, this is a non-critical error (for ease-of-use i want nodes to start this server if it is not running already but if it is already running should ignore this error)
		// so make an exception for this event
		errString := err.Error()

		if len(errString) >= 22 { // safely access last 22 chars if the error string is at least that long
			if errString[len(errString)-22:] == "address already in use" {
				logger.L.Println("Perforance Stats server is already running.")
				return
			}
		}

		// other errors should be printed but should not lead to the crash of the node
		logger.L.Printf("StartPerformanceStatsServer - ERROR: %v", err)
	}

}

// ---- Custom data type ----

// Event is an int alias later used to created an enum that describes which kind of event occurred that the stats are reported for
type Event int

const (
	Event_Libp2pNodeIDKnown    		Event = iota + 71 	// you know now your libp2p node ID (either it was read from file or new identity was created)
	Event_FirstPeerConnected        					// you have connected to your first peer (Note: If this peer is the RA the event RAConnected will not be reported again)
	Event_NewPeerConnected								// you have connected to a new peer (but it is not your first one)
	Event_RAConnected 									// you have connected to the RA
	Event_InitialSyncAlmostCompleted  					// you have synced to other nodes but you do not know the current BPH or currently pending transactions. You will still need to request them
	Event_InititalSyncCompleted 						// you have completed your initial sync
	Event_SimtaskReceived 								// you have a new simtask (new block problem) from the RA
	Event_TransactionReceived 							// you have received a new transaction via topic
	Event_MinerCommitReceived 							// you have received a new miner commitment (a miner claims to have solved the current block problem and provides 'zero' knowledge proof)
	Event_RACommitRevealed 								// you have received RA secret bytes reveal (necessary for everyone to select winner)
	Event_BlockWinnerFound 								// you YOURSELF have determined who will be the next block winner (if everything works every node should always choose the same block winner)
	Event_NewBlockReceived 								// you have received a new block from the RA (light nodes also receive a full block but then only store its header)

	Event_RANewProblemSent								// RA only: you have successfully sent a new block problem to the respective topic
	Event_RANewBlockSent								// RA only: you have successfully created and sent a new block to the respective topic
	Event_RACommitRevealSent							// RA only: you have successfully sent your secret bytes reveal to the respective topic
	Event_RAReceivedSimulationData 						// RA only: a miner sent you their block problem solution
)

// String implements the string interface for Event so that the name of the enum element will be printed instead of its int value.
func (e Event) String() string {
	switch e {

	case Event_Libp2pNodeIDKnown:
		return "Event_Libp2pNodeIDKnown"
	case Event_FirstPeerConnected:
		return "Event_FirstPeerConnected"
	case Event_NewPeerConnected:
		return "Event_NewPeerConnected"
	case Event_RAConnected:
		return "Event_RAConnected"
	case Event_InitialSyncAlmostCompleted:
		return "Event_InitialSyncAlmostCompleted"
	case Event_InititalSyncCompleted:
		return "Event_InititalSyncCompleted"
	case Event_SimtaskReceived:
		return "Event_SimtaskReceived"
	case Event_TransactionReceived:
		return "Event_TransactionReceived"
	case Event_MinerCommitReceived:
		return "Event_MinerCommitReceived"
	case Event_RACommitRevealed:
		return "Event_RACommitRevealed"
	case Event_BlockWinnerFound:
		return "Event_BlockWinnerFound"
	case Event_NewBlockReceived:
		return "Event_NewBlockReceived"
	
	// RA only:
	case Event_RANewProblemSent:
		return "Event_RANewProblemSent"
	case Event_RANewBlockSent:
		return "Event_RANewBlockSent"
	case Event_RACommitRevealSent:
		return "Event_RACommitRevealSent"
	case Event_RAReceivedSimulationData:
		return "Event_RAReceivedSimulationData"

	default:
		return fmt.Sprintf("%d", e)
	}
}

// PerformanceData holds relevant data of a measurement in string form. Useful as the JS code that will handle the received data only has to handle strings. 
type PerformanceData struct {
	Time 		string `json:"time"`  		// Example: whatever is returned by time.Now().UnixNano() as string
	Sender 		string `json:"sender"`		// Example: this refer to the docker container name, e.g. "1f". Default value when flag -dockerAlias not set: "mynode"
	EventType 	string `json:"eventtype"`
	Message  	string `json:"message"` 	// Example: You can put additional info here. I usually put the libp2p node ID of the node I just connected to, or a bogus value for the completed initial sync (field is not allowed to be empty)
}

// NewPerformanceData is the constructor used for holding PerformanceData. It actually converts the given data to string and returns an instance of PerformanceData.
func NewPerformanceData(currentTime int64, senderNode string, eventType Event, message string) PerformanceData {
	return PerformanceData {
		Time: 		fmt.Sprintf("%v", currentTime),
		Sender: 	senderNode,
		EventType: 	eventType.String(),
		Message: 	message,
	}
}
