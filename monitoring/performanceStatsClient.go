package monitoring

import (
	"bytes"
	"fmt"
	unsafeRand "math/rand" // used to get non-cryptographically secure pseudo-random numbers used for sleeping when needed (e.g. networking-related request failed and should be tried again after short delay)
	"net/http"
	"net/url"
	"time"

	"github.com/felix314159/gophy/logger"
)

/* EXAMPLE USAGE:
// get message to send
curtime := time.Now().UnixNano()
sender 	:= "1f"
eventtype := Event_InitialSyncCompleted
msg := "test"
p := NewPerformanceData(curtime, sender, eventtype, msg)

// send message
err := SendPerformanceStat(p)
if err != nil {
	logger.L.Println(err)
	return
}

logger.L.Printf("Successfully sent performance stat data to localhost:%v and got server ACK.\n", serverPort)
*/

// performanceStatReportingUpperCap defines how often a node will try to report some performance stat to the server when it keeps failing
const performanceStatReportingUpperCap = 50

// SendPerformanceStat is the function implicitely (you are supposed to call SendPerformanceStatWithRetries) to transmit their collected performance data to the server run at localhost:12345
// Return nil when stat was successfully reported to the server and an acknowledgement of that was received. Otherwise returns an error message.
func SendPerformanceStat(p PerformanceData) error {
	// serverURL holds target address where to send messages
	serverURL := fmt.Sprintf("http://localhost:%v/send", serverPort)

	// construct message
	data := url.Values{}
	data.Set("time", p.Time)
	data.Set("sender", p.Sender)
	data.Set("eventtype", p.EventType)
	data.Set("message", p.Message)

	// POST message and get server reply
	resp, err := http.PostForm(serverURL, data)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	buf := new(bytes.Buffer)
	_, err = buf.ReadFrom(resp.Body)
	if err != nil {
		return fmt.Errorf("Failed to get performance stats server reply: %v", err)
	}

	// ensure server response is ACK
	respString := buf.String()
	if respString != "ACK" {
		return fmt.Errorf("Expected performance stats server reply 'ACK' but instead got reply: %v", respString)
	}

	return nil
}

// SendPerformanceStatWithRetries is the function used by nodes to report performance stats. Should it fail (e.g. packet gets lost or no ACK from stats server received), it will try again but only up to performanceStatReportingUpperCap times often. If no retries are available and error still is not nil, then an error message is returned.
func SendPerformanceStatWithRetries(p PerformanceData) error {
	retryCounter := 0
	err := SendPerformanceStat(p)
	for ((err != nil) && (retryCounter < performanceStatReportingUpperCap)) {
		logger.L.Printf("Failed to report performance stat: %v\nWill try again..", err)

		// try again after short sleep
		RandomShortSleep()
		retryCounter += 1
		err = SendPerformanceStat(p)
	}
	if err != nil {
		return fmt.Errorf("Even after retrying %v times I was not able to report the performance stat successfully. Too bad, but I will not panic", performanceStatReportingUpperCap)
	}

	return nil
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