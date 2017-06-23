package main

import (
	"bufio"
	"flag"
	"log"
	"net/http"
	"os"
	"time"
)

const (
	numPollers     = 2                // number of Poller goroutines to launch
	pollInterval   = 60 * time.Second // how often to poll each URL
	statusInterval = 10 * time.Second // how often to log status to stdout
	errTimeout     = 10 * time.Second // back-off timeout on error
)

var urlsFilepath = flag.String("urlsFilepath", "", "filepath to .txt file containing urls to poll (each on new line)")

// State represents the last-known state of a URL.
type State struct {
	url    string
	status string
}

// StateMonitor maintains a map that stores the state of the URLs being
// polled, and prints the current state every updateInterval nanoseconds.
// It returns a chan State to which resource state should be sent.
func StateMonitor(updateInterval time.Duration) chan<- State {
	updates := make(chan State)
	urlStatus := make(map[string]string)
	ticker := time.NewTicker(updateInterval)
	go func() {
		for {
			select {
			case <-ticker.C:
				logState(urlStatus)
			case s := <-updates:
				urlStatus[s.url] = s.status
			}
		}
	}()
	return updates
}

// logState prints a state map.
func logState(s map[string]string) {
	log.Println("Current state:")
	for k, v := range s {
		log.Printf(" %s %s", k, v)
	}
}

// Resource represents and HTTP URL to be polled by this program.
type Resource struct {
	url      string
	errCount int
}

// Poll executes an HTTP HEAD request for url
// and returns the HTTP status string or an error string.
func (r *Resource) Poll() string {
	resp, err := http.Head(r.url)
	if err != nil {
		log.Println("Error", r.url, err)
		r.errCount++
		return err.Error()
	}
	r.errCount = 0
	return resp.Status
}

// Sleep sleeps for an appropriate interval (dependent on error state)
// before sending the Resource to done.
func (r *Resource) Sleep(done chan<- *Resource) {
	time.Sleep(pollInterval + errTimeout*time.Duration(r.errCount))
	done <- r
}

// Poller receives a Resource from in, records the status of its URL in status,
// and then releases the Resource back through out
func Poller(in <-chan *Resource, out chan<- *Resource, status chan<- State) {
	for r := range in {
		s := r.Poll()
		status <- State{r.url, s}
		out <- r
	}
}

// Sender reads urls from provided filepath and sends them as Resources
// to the queue receiving poll requests
func Sender(urlsFilepath string, todo chan<- *Resource) {
	urlsFile, err := os.Open(urlsFilepath)
	if err != nil {
		log.Fatalln("failed to read urls from file", urlsFilepath, err)
		return
	}
	defer urlsFile.Close()

	for scanner := bufio.NewScanner(urlsFile); scanner.Scan(); {
		todo <- &Resource{url: scanner.Text()}
	}
}

func main() {
	// Parse command-line flags
	flag.Parse()

	// Validate urls filepath
	if *urlsFilepath == "" {
		log.Fatalln("failed to provide valid urls filepath")
		return
	}

	// Create our input and output channels
	pending, complete := make(chan *Resource), make(chan *Resource)

	// Launch the StateMonitor
	status := StateMonitor(statusInterval)

	// Launch some Poller goroutines
	for i := 0; i < numPollers; i++ {
		go Poller(pending, complete, status)
	}

	// Send some Resources to the pending queue
	go Sender(*urlsFilepath, pending)

	// Re-deliver polled Resources back to pending queue after sleep duration
	for r := range complete {
		go r.Sleep(pending)
	}
}
