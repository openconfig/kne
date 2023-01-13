// Copyright 2023 Google LLC
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//      http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

// Package logshim provides a shim to send an io.Writer to a logger, one line at
// a time.  Normally only lines terminated with a newline are sent.  If a
// partial line is sent it will be buffered for up to one minute waiting for the
// rest of the line.  After one minute it will be sent as a line.
package logshim

import (
	"bytes"
	"sync"
	"time"
)

type shim struct {
	mu       sync.Mutex
	partial  []byte
	timer    *time.Timer
	cancel   chan struct{} // Closed when Close is called
	canceled chan struct{} // Closed when final data is flushed
	write    func(...interface{})
}

const flushTimeout = time.Minute

var testChannel chan struct{}

// New returns an io.Writer that sends complete lines to the function f.  Each
// complete line is written to f individually. Incomplete lines way up to one
// minute for additional data before flushing in the background.
func New(f func(...interface{})) *shim {
	s := &shim{
		timer:    time.NewTimer(time.Hour),
		cancel:   make(chan struct{}),
		canceled: make(chan struct{}),
		write:    f,
	}
	s.timer.Stop()
	go func() {
		for {
			select {
			case <-s.timer.C:
				s.flush()
			case <-s.cancel:
				s.flush()
				close(s.canceled)
				return
			}
		}
	}()
	return s
}

// Close releases resources used by s.  Close does not return until any buffered
// data has been written.
func (s *shim) Close() {
	close(s.cancel)
	<-s.canceled
}

// Write implements io.Writer.
func (s *shim) Write(buf []byte) (int, error) {
	n := len(buf)

	// Prepend anything leftover from the previous write and then split into
	// lines.  If buf does not terminate in a newline then save the last
	// line to prefix the next write.
	s.mu.Lock()
	buf = append(s.partial, buf...)
	lines := bytes.Split(buf, []byte{'\n'})

	if buf[len(buf)-1] != '\n' {
		s.partial = lines[len(lines)-1]
		lines = lines[:len(lines)-1]
		s.timer.Stop()
		s.timer.Reset(flushTimeout)
	} else {
		s.partial = nil
	}
	s.mu.Unlock()

	for _, line := range lines {
		s.write(string(line))
	}
	return n, nil
}

// flush flushes any partial output.
func (s *shim) flush() {
	s.mu.Lock()
	s.timer.Stop()
	line := s.partial
	s.partial = nil
	s.mu.Unlock()

	if len(line) > 0 {
		s.write(string(line))
	}
	if testChannel != nil {
		close(testChannel)
		testChannel = nil
	}
}
