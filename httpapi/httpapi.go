// Package httpapi contains code that is related to the html websites used for defining and sending transactions and simtasks.
package httpapi

import (
	"context"
	"encoding/json"
	"fmt"
	"html/template"
	"net/http"
	"path/filepath"

	"github.com/felix314159/gophy/block/simpar"
	"github.com/felix314159/gophy/database"
	"github.com/felix314159/gophy/logger"
)

// httpApiFolder holds the OS-agnostic path to the folder of httpapi
var httpApiFolder = filepath.Join(".", "httpapi")

// ResponseData holds the response data which is sent as JSON
type ResponseData struct {
    Message string `json:"message"`
}

// ---- Start of Send Transaction website ----

// displayWebsiteTransactions displays the website
func displayWebsiteTransactions(w http.ResponseWriter, r *http.Request) {
    t, err := template.ParseFiles(filepath.Join(httpApiFolder, "submitTransaction.html"))
    if err != nil {
        http.Error(w, err.Error(), http.StatusInternalServerError)
        return
    }
    t.Execute(w, nil)
}

// handleFormTransactions processes the input data and displays the respose
func handleFormTransactions(w http.ResponseWriter, r *http.Request) {
    // we are only interested in POST methods, so ignore the rest by redirecting to home page
    if r.Method != http.MethodPost {
        http.Redirect(w, r, "/", http.StatusSeeOther) // StatusSeeOther is code 303 (you have been redirected to home, so now make GET request to load that website)
        return
    }

    // get user input values of form defined in html file
    toAddress := r.FormValue("toAddress")
    value := r.FormValue("value")
    fee := r.FormValue("fee")
    reference := r.FormValue("reference")
    
    logger.L.Printf("\nYou intend to send this transaction:\n\tTo: %v\n\tAmount: %v\n\tFee: %v\n\tReference: %v\n", toAddress, value, fee, reference)

    // check whether the transaction entered by the user is valid
    transactionIsValid, transactionObj := database.TransactionIsValid(toAddress, value, reference, fee)

    // display failed transaction on website
    if !transactionIsValid {
        response := ResponseData {
            Message: fmt.Sprintf("Transaction is invalid and has not been sent! Check terminal for details."),
        }
        w.Header().Set("Content-Type", "application/json")
        json.NewEncoder(w).Encode(response)

        return
    }

    // transaction is valid, so send it and then inform user
    //      prepare to send transaction
    //              1. serialize it
    transactionSer, err := database.TransactionToBytes(transactionObj)
    if err != nil {
        logger.L.Panicf("Failed to serialize transaction: %v", err)
    }
    //              2. wrap it in TS
    transactionTS := database.NewTransportStruct(database.TSData_Transaction, database.MyNodeIDString, transactionSer)
    //      send transaction to topic 'pouw_transactions'
    ctxbg := context.Background()
    err = database.TopicSendMessage(ctxbg, "pouw_transactions", transactionTS)
    for err != nil { // for some reason transaction failed to send
        // sending failed, so show that on the website. but also try to re-send until it works
        response := ResponseData {
            Message: fmt.Sprintf("Failed to send transaction. Check terminal for details."),
        }
        w.Header().Set("Content-Type", "application/json")
        json.NewEncoder(w).Encode(response)


        // try sending again
        logger.L.Printf("Failed to send topic message: %v\nWill try again..\n", err)
        database.RandomShortSleep()
        err = database.TopicSendMessage(ctxbg, "pouw_transactions", transactionTS)
    }

    // transaction has successfully been sent (Note: it will only be added to list of pending transactions after it actually has been received via the topic)
    response := ResponseData {
        Message: fmt.Sprintf("Transaction has been sent."),
    }
    w.Header().Set("Content-Type", "application/json")
    json.NewEncoder(w).Encode(response)

}

// ---- Start of Send Simtask website ----

// displayWebsiteSimtask displays the website
func displayWebsiteSimtask(w http.ResponseWriter, r *http.Request) {
    t, err := template.ParseFiles(filepath.Join(httpApiFolder, "submitSimtask.html"))
    if err != nil {
        http.Error(w, err.Error(), http.StatusInternalServerError)
        return
    }
    t.Execute(w, nil)
}

// handleFormSimtask processes the input data and displays the respose
func handleFormSimtask(w http.ResponseWriter, r *http.Request) {
    // we are only interested in POST methods, so ignore the rest by redirecting to home page
    if r.Method != http.MethodPost {
        http.Redirect(w, r, "/", http.StatusSeeOther) // StatusSeeOther is code 303 (you have been redirected to home, so now make GET request to load that website)
        return
    }

    // parse form data
    if err := r.ParseForm(); err != nil {
        http.Error(w, err.Error(), http.StatusInternalServerError)
        logger.L.Println("handleFormSimtask - FAILED TO PARSE FORM!")
        return
    }

    // iterate over dynamic fields to retrieve each field content as string
    subproblemCount := 1
    allSubproblemsAreValid := true
    var simparObjectSlice []simpar.SimulationParameters // hold simpars that have passed validity check
    for {
        amountEvents := r.FormValue(fmt.Sprintf("amountEvents%d", subproblemCount))
        runID := r.FormValue(fmt.Sprintf("runID%d", subproblemCount))
        particleID := r.FormValue(fmt.Sprintf("particleID%d", subproblemCount))
        momentum := r.FormValue(fmt.Sprintf("momentum%d", subproblemCount))
        theta := r.FormValue(fmt.Sprintf("theta%d", subproblemCount))

        // we try to access form values until every field is empty (which means that either we have already collected all form values or someone left a completely empty subproblem in the middle. in the latter case all following subproblems are IGNORED and not part of the simtask)
        if amountEvents == "" && runID == "" && particleID == "" && momentum == "" && theta == "" {
            break // we had already reached the last subproblem OR someone left an empty subproblem in the middle, so we will ignore all following subproblems
        }

        // however, if a subproblem that contains at least one non-empty field but also at least one empty field is invalid, in such a case the entire set of subproblems is denied
        if amountEvents == "" || runID == "" || particleID == "" || momentum == "" || theta == "" {
            logger.L.Printf("ERROR: There exists at least one subproblem which is invalid because at least one of its field is empty. Will ignore entire simtask submission! One of these fields is problematic:\nAmount Events: %v\nRun ID: %v\nParticle ID: %v\nMomentum: %v\nTheta: %v\n", amountEvents, runID, particleID, momentum, theta)
            allSubproblemsAreValid = false
            break
        }

        logger.L.Printf("httpapi received a new set of parameters:\nSubproblem %d:\n\tAmount of Events: %v\n\tRun ID: %v\n\tParticle ID: %v\n\tMomentum: %v\n\tTheta: %v\n",
            subproblemCount, amountEvents, runID, particleID, momentum, theta)

        // try to get simpar object if these values are valid
        subproblemIsValid, simparObject := database.SubproblemIsValid(amountEvents, runID, particleID, momentum, theta)
        if !subproblemIsValid {
            logger.L.Printf("ERROR: This subproblem is invalid because at least one field contains invalid data. Will ignore entire simtask submission!")
            allSubproblemsAreValid = false
            break
        }

        // hold valid simpar object in slice
        simparObjectSlice = append(simparObjectSlice, simparObject)

        subproblemCount++
    }


    // display server response on website (passes it to html file as JSON object)
    var response ResponseData
    if allSubproblemsAreValid {
        response = ResponseData {
            Message: fmt.Sprintf("Simtask has been added to queue!"),
        }
    } else {
        response = ResponseData {
            Message: fmt.Sprintf("Simtask has not been added to queue because at least one field contains invalid data!"),
        }
    }
    w.Header().Set("Content-Type", "application/json")
    json.NewEncoder(w).Encode(response)

    // only now actually add simpar slice to the queue if every subproblem is valid
    if allSubproblemsAreValid {
        database.AddPendingSimpar(simparObjectSlice)
    }
    
}

// RunServer runs two local websites on port database.HttpPort that can be used via browser to send transactions or broadcast new simtasks
func RunServer() {
    // configure transaction website
    patternPrefixTransactions := "/send-transaction"
    http.HandleFunc(patternPrefixTransactions, displayWebsiteTransactions)  // define website to host 
    http.HandleFunc("/submitTransaction", handleFormTransactions)           // bind the form called /submit to the function handleForm (pattern given here must be unique)
    
    // configure simtask website
    patternPrefixSimtask := "/send-simtask"
    http.HandleFunc(patternPrefixSimtask, displayWebsiteSimtask)
    http.HandleFunc("/submitSimtask", handleFormSimtask)

    logger.L.Printf("\nStarting transaction website at localhost:%v%v\nStarting simtask website at localhost:%v%v\n\n", database.HttpPort, patternPrefixTransactions, database.HttpPort, patternPrefixSimtask)
    
    // run websites as a goroutine
    go func() {
        // Will panic if there are issues such as e.g. trying to run multiple instances of this pool using the same port, ensure that in these cases you use the port flag to explicitely set a different, unused port
        panic(http.ListenAndServe(fmt.Sprintf("localhost:%v", database.HttpPort), nil))  // run server on localhost=loopback interface (important to use localhost instead of your IP address so that website is only available from this machine, otherwise everyone on local network could e.g. send transactions via your wallet)
    }()

    //select{} // don't terminate the server (only needed when server is run as standalone program and main would terminate)
}
