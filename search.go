package main

import (
	"bufio"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"sync"
)

// Phases shown to the user.
const (
	phaseIdle     = "idle"
	phaseSetup    = "setup"
	phaseStarting = "starting"
	phaseSearch   = "searching"
	phaseDone     = "done"
	phaseError    = "error"
)

// Status is what the page polls to draw the progress line.
type Status struct {
	Phase    string `json:"phase"`
	Message  string `json:"message"`
	Found    int    `json:"found"`
	Checked  int    `json:"checked"`
	Leads    int    `json:"leads"`
	Running  bool   `json:"running"`
	FileName string `json:"fileName,omitempty"`
}

// searcher runs one search at a time (in a worker process) and keeps the
// latest results.
type searcher struct {
	mu       sync.Mutex
	status   Status
	cmd      *exec.Cmd
	stopped  bool
	leads    []Lead
	seen     map[string]bool
	fileName string
	errMsg   string
}

func newSearcher() *searcher {
	return &searcher{status: Status{Phase: phaseIdle}}
}

func (s *searcher) Status() Status {
	s.mu.Lock()
	defer s.mu.Unlock()

	st := s.status
	st.Leads = len(s.leads)

	return st
}

// CSV returns the current sheet and a friendly file name.
func (s *searcher) CSV() ([]byte, string, error) {
	s.mu.Lock()
	leads := append([]Lead(nil), s.leads...)
	name := s.fileName
	s.mu.Unlock()

	if name == "" {
		name = "leads.csv"
	}

	data, err := buildCSV(leads)

	return data, name, err
}

// Start launches a search in the background. It returns an error if one is
// already running or the input is invalid.
func (s *searcher) Start(business, city string, howMany int) error {
	business = strings.TrimSpace(business)
	city = strings.TrimSpace(city)

	if business == "" || city == "" {
		return errors.New("please fill in both the business and the city")
	}

	howMany = max(1, min(howMany, maxPlaces))

	exe, err := os.Executable()
	if err != nil {
		return err
	}

	cmd := exec.Command(exe, "--worker",
		"--query", business+" in "+city,
		"--town", city,
		"--count", strconv.Itoa(howMany),
	)
	cmd.Stderr = os.Stderr

	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return err
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	if s.status.Running {
		return errors.New("a search is already running")
	}

	if err := cmd.Start(); err != nil {
		return fmt.Errorf("could not start the search: %w", err)
	}

	s.cmd = cmd
	s.stopped = false
	s.errMsg = ""
	s.leads = nil
	s.seen = map[string]bool{}
	s.fileName = fileNameFor(business, city)
	s.status = Status{Phase: phaseStarting, Message: "Starting…", Running: true, FileName: s.fileName}

	go s.follow(cmd, stdout)

	return nil
}

// follow reads the worker's progress until it exits.
func (s *searcher) follow(cmd *exec.Cmd, stdout io.Reader) {
	sc := bufio.NewScanner(stdout)
	sc.Buffer(make([]byte, 64*1024), 1024*1024)

	for sc.Scan() {
		line := sc.Text()

		payload, ok := strings.CutPrefix(line, eventPrefix)
		if !ok {
			fmt.Fprintln(os.Stderr, line)
			continue
		}

		var ev event
		if err := json.Unmarshal([]byte(payload), &ev); err != nil {
			continue
		}

		s.apply(ev)
	}

	waitErr := cmd.Wait()

	s.mu.Lock()
	defer s.mu.Unlock()

	s.cmd = nil
	s.status.Running = false

	switch {
	case s.stopped:
		s.status.Phase = phaseDone
		s.status.Message = fmt.Sprintf("Stopped. Checked %d places, found %d leads with no website.", s.status.Checked, len(s.leads))
	case s.errMsg != "" || waitErr != nil:
		msg := s.errMsg
		if msg == "" {
			msg = waitErr.Error()
		}

		log.Printf("search failed: %s", msg)
		s.status.Phase = phaseError
		s.status.Message = "Something went wrong: " + msg
	default:
		s.status.Phase = phaseDone
		s.status.Message = fmt.Sprintf("Done! Checked %d places, found %d leads with no website.", s.status.Checked, len(s.leads))
	}
}

func (s *searcher) apply(ev event) {
	s.mu.Lock()
	defer s.mu.Unlock()

	switch ev.Type {
	case "phase":
		s.status.Phase = ev.Phase
		s.status.Message = ev.Message
	case "found":
		s.status.Found += ev.N
	case "checked":
		s.status.Checked += ev.N
	case "place":
		if ev.Place == nil || s.seen[ev.Key] {
			return
		}

		s.seen[ev.Key] = true
		s.leads = append(s.leads, *ev.Place)
	case "error":
		s.errMsg = ev.Message
	}
}

// Stop ends the running search; results found so far are kept.
func (s *searcher) Stop() {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.cmd != nil && s.cmd.Process != nil {
		s.stopped = true
		_ = s.cmd.Process.Kill()
	}
}

func fileNameFor(business, city string) string {
	clean := func(s string) string {
		var b strings.Builder

		for _, r := range strings.ToLower(s) {
			switch {
			case r >= 'a' && r <= 'z', r >= '0' && r <= '9':
				b.WriteRune(r)
			case r == ' ' || r == '-' || r == '_':
				b.WriteRune('-')
			}
		}

		return strings.Trim(b.String(), "-")
	}

	name := "leads"
	if b := clean(business); b != "" {
		name += "-" + b
	}

	if c := clean(city); c != "" {
		name += "-" + c
	}

	return name + ".csv"
}
