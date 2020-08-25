package main

import (
	"github.com/rfizzle/collector-helpers/outputs"
	"github.com/rfizzle/collector-helpers/state"
	"github.com/rfizzle/microsoft-graph-collector/client"
	log "github.com/sirupsen/logrus"
	"github.com/spf13/viper"
	"os"
	"time"
)

func main() {
	// Setup variables
	var maxMessages = int64(5000)

	// Setup logging
	log.SetFormatter(&log.TextFormatter{
		FullTimestamp: true,
	})
	log.SetOutput(os.Stdout)

	// Setup Parameters via CLI or ENV
	if err := setupCliFlags(); err != nil {
		log.Errorf("initialization failed: %v", err.Error())
		os.Exit(1)
	}

	// Set log level based on supplied verbosity
	if viper.GetBool("verbose") {
		log.SetLevel(log.DebugLevel)
	} else {
		log.SetLevel(log.InfoLevel)
	}

	// Setup log writer
	tmpWriter, err := outputs.NewTmpWriter()
	if err != nil {
		log.Errorf("%v", err.Error())
		os.Exit(1)
	}

	// Setup the channels for handling async messages
	chnMessages := make(chan string, maxMessages)

	// Setup the Go Routine
	pollTime := viper.GetInt("schedule")

	// Start Poll
	go pollEvery(pollTime, chnMessages, tmpWriter)

	// Handle messages in the channel (this will keep the process running indefinitely)
	for message := range chnMessages {
		handleMessage(message, tmpWriter)
	}
}

func pollEvery(seconds int, resultsChannel chan<- string, tmpWriter *outputs.TmpWriter) {
	var currentState *state.State
	var err error

	// Setup State
	if state.Exists(viper.GetString("state-path")) {
		currentState, err = state.Restore(viper.GetString("state-path"))
		if err != nil {
			log.Errorf("error getting state: %v", err.Error())
			os.Exit(1)
		}
	} else {
		currentState = state.New()
	}

	// Poll every
	for {
		log.Println("getting microsoft graph security events...")

		// Get events
		eventCount, lastPollTime, err := getEvents(currentState.LastPollTimestamp, resultsChannel)

		// Handle error
		if err != nil {
			// Wait for x seconds and retry poll
			<-time.After(time.Duration(seconds) * time.Second)

			// Retry the request
			continue
		}

		// Copy tmp file to correct outputs
		if eventCount > 0 {
			// Wait until the results channel has no more messages 0
			for len(resultsChannel) > 0 {
				<-time.After(time.Duration(1) * time.Second)
			}

			// Close and rotate file
			_ = tmpWriter.Rotate()

			// Write to enabled outputs
			if err := outputs.WriteToOutputs(tmpWriter.LastFilePath, lastPollTime.Format(time.RFC3339)); err != nil {
				log.Errorf("unable to write to output: %v", err)
			}

			// Remove temp file now
			err := os.Remove(tmpWriter.LastFilePath)
			if err != nil {
				log.Errorf("unable to remove tmp file: %v", err)
			}
		}

		// Let know that event has been processes
		log.Infof("%v events processed", eventCount)

		// Update state
		currentState.LastPollTimestamp = lastPollTime.Format(time.RFC3339)
		state.Save(currentState, viper.GetString("state-path"))

		// Wait for x seconds until next poll
		<-time.After(time.Duration(seconds) * time.Second)
	}
}

func getEvents(timestamp string, resultChannel chan<- string) (int, time.Time, error) {
	// Get current time
	now := time.Now()

	// Build an HTTP client with JWT header
	graphClient, err := client.NewClient(viper.GetString("tenant-id"), viper.GetString("client-id"), viper.GetString("client-secret"))

	// Handle error
	if err != nil {
		log.Errorf("unable to build client: %v", err)
		return 0, now, err
	}

	// Get alerts
	dataCount, err := graphClient.GetAlerts(timestamp, now.Format(time.RFC3339), resultChannel)

	// Return error
	if err != nil {
		log.Errorf("error getting alerts: %v", err)
		return 0, now, err
	}

	// Return count and timestamp
	return dataCount, now, nil
}

// Handle message in a channel
func handleMessage(message string, tmpWriter *outputs.TmpWriter) {
	if err := tmpWriter.WriteLog(message); err != nil {
		log.Errorf("unable to write to temp file: %v", err)
	}
}
