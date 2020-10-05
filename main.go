package main

import (
	"github.com/rfizzle/collector-helpers/outputs"
	"github.com/rfizzle/collector-helpers/state"
	"github.com/rfizzle/microsoft-graph-collector/client"
	log "github.com/sirupsen/logrus"
	"github.com/spf13/viper"
	"os"
	"os/signal"
	"sync"
	"syscall"
	"time"
)

func main() {
	// Setup wait group for no closures
	var wg sync.WaitGroup
	wg.Add(1)

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
	logger := &outputs.TmpWriter{}

	// Setup the channels for handling async messages
	chnMessages := make(chan string, maxMessages)

	// Setup the Go Routine
	pollTime := viper.GetInt("schedule")

	// Soft close when CTRL + C is called
	done := setupCloseHandler()

	// Let the user know the collector is starting
	log.Infof("starting collector...")

	// Start Poll
	go pollEvery(pollTime, chnMessages, logger, done)

	// Handle messages
	go func() {
		for {
			message, ok := <-chnMessages
			if !ok {
				log.Debugf("closed channel, doing cleanup...")
				cleanupProcedure(logger)
				wg.Done()
				return
			} else {
				handleMessage(message, logger)
			}
		}
	}()

	wg.Wait()
}

// Goroutine poll for collecting events
func pollEvery(seconds int, resultsChannel chan<- string, logger *outputs.TmpWriter, done chan bool) {
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

	for {
		select {
		case <-done:
			log.Debugf("closing go routine...")
			close(resultsChannel)
			return
		case <-time.After(time.Duration(seconds) * time.Second):
			log.Infof("getting microsoft graph security events...")

			// Get events
			eventCount, lastPollTime, err := getEvents(currentState.LastPollTimestamp, resultsChannel)

			// Handle error
			if err != nil {
				// Retry the request
				continue
			}

			// Copy tmp file to correct outputs
			if eventCount > 0 {
				// Wait until the results channel has no more messages and all writes have completed
				for len(resultsChannel) > 0 || logger.WriteCount != eventCount {
					<-time.After(time.Duration(50) * time.Millisecond)
				}

				// Close and rotate file
				err = logger.Rotate()

				// Handle error
				if err != nil {
					log.Errorf("unable to rotate file")
					continue
				}

				// Get stats on source file
				sourceFileStat, err := os.Stat(logger.PreviousFile().Name())
				if err != nil {
					log.Errorf("error reading last file path")
					continue
				}

				// Continue if source file size is 0 (technically this should never happen if there are events)
				if sourceFileStat.Size() == 0 {
					log.Errorf("tmp file is 0 bytes with events")
					_ = logger.DeletePreviousFile()
					continue
				}

				// Write to enabled outputs
				if err := outputs.WriteToOutputs(logger.PreviousFile().Name(), lastPollTime.Format(time.RFC3339)); err != nil {
					log.Errorf("unable to write to output: %v", err)
				}

				// Remove temp file now
				err = logger.DeletePreviousFile()
				if err != nil {
					log.Errorf("unable to remove tmp file: %v", err)
				}
			}

			// Let know that event has been processes
			log.Infof("%v events processed", eventCount)

			// Update state
			currentState.LastPollTimestamp = lastPollTime.Format(time.RFC3339)
			state.Save(currentState, viper.GetString("state-path"))
		}
	}
}

// Get events
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
func handleMessage(message string, logger *outputs.TmpWriter) {
	if _, err := logger.WriteString(message); err != nil {
		log.Errorf("unable to write to temp file: %v", err)
	}
}

// SetupCloseHandler creates a 'listener' on a new goroutine which will notify the
// program if it receives an interrupt from the OS.
func setupCloseHandler() chan bool {
	done := make(chan bool)
	c := make(chan os.Signal)
	signal.Notify(c, os.Interrupt, syscall.SIGTERM)
	go func() {
		<-c
		done <- true
	}()

	return done
}

// Cleanup collector tmp files
func cleanupProcedure(w *outputs.TmpWriter) {
	// Remove last temp file
	log.Debugf("removing temp files...")
	if err := w.Exit(); err != nil {
		log.Errorf("unable to close tmp writer successfully: %v", err)
	}

	// Close message
	log.Infof("collector closed successfully...")
}
