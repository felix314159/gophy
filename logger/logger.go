// Package logger is used to log messages both to stdout and to a log file. The two main reasons to use this over fmt:
//      - concurrency-safe (messages are shown in correct order which is not the case with fmt)
//      - log to terminal and file at the same time across multiple goroutines (persistent logs are useful when so much output is printed that you can't scroll up all the way in the terminal)
// Example Usage:
//      logger.L.Printf("this is %v example, %v", a, b)
//      logger.L.Panicf("we crashed and look at this parameter %v", a)
package logger

import (
    "io"
    "log"
    "os"
)

const logFilePath = "app.log" // its in rootdir, or when running tests in every cwd

var (
    // L is the instance of the logger used across the program
    L *log.Logger
)

func init() { // init is automatically run if this package is imported
    // delete old log file if it exists
    err := os.Remove(logFilePath)  // ignore err that is returned (either file is deleted or file doesnt exist. either way file won't exist after this)
    if err != nil {
        // only panic if the error is something other than "file does not exist"
        if !os.IsNotExist(err) {
            panic(err)
        } 

    }

    // open log file
    logFile, err := os.OpenFile(logFilePath, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0666) // append to log if it exists, otherwise create it
    if err != nil {
        log.Fatalln("Failed to open log file:", err)
    }
    
    // set logger to log both to file and to stdout
    L = log.New(io.MultiWriter(logFile, os.Stdout), "", log.Ldate|log.Ltime|log.Lshortfile)
}
