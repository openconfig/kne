// Copyright 2026 Google LLC
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

package bridge

import (
	"fmt"
	"sync"

	"github.com/vishvananda/netlink"
	"golang.org/x/sys/unix"
	"k8s.io/klog/v2"

	wpb "github.com/openconfig/kne/proto/wire"
)

// LinkObservation is the state of a local network interface.
type LinkObservation struct {
	// AdminUp reports whether the interface is administratively up (IFF_UP).
	AdminUp bool
	// Carrier reports whether the interface has carrier (IFF_LOWER_UP). On a
	// meshnet link it is true only while the device at the other end of the
	// link is administratively up.
	Carrier bool
}

// Link controls and observes the link state of one local network interface.
//
// A bridge propagates link state across a Wire by setting its interface
// administratively up or down, which the device at the other end of the link
// sees as a carrier change, and by reporting the carrier of its interface,
// which reflects the admin state of that device.
type Link interface {
	// SetAdminUp sets the interface administratively up or down.
	SetAdminUp(up bool) error
	// Current returns the current state of the interface.
	Current() (LinkObservation, error)
	// Events returns a channel of state changes. Changes may be coalesced, so
	// a reader sees the latest state but not necessarily every transition.
	// The channel is closed by Close.
	Events() <-chan LinkObservation
	// Close stops observing the interface.
	Close() error
}

// observationFromFlags converts interface flags to a LinkObservation.
func observationFromFlags(flags uint32) LinkObservation {
	return LinkObservation{
		AdminUp: flags&unix.IFF_UP != 0,
		Carrier: flags&unix.IFF_LOWER_UP != 0,
	}
}

// netlinkLink is a Link for an interface in the current network namespace.
type netlinkLink struct {
	ifName    string
	index     int
	events    chan LinkObservation
	done      chan struct{}
	finished  chan struct{}
	closeOnce sync.Once
}

// NewNetlinkLink returns a Link for the named interface in the current network
// namespace.
func NewNetlinkLink(ifName string) (Link, error) {
	l, err := netlink.LinkByName(ifName)
	if err != nil {
		return nil, fmt.Errorf("interface %s not found: %w", ifName, err)
	}
	updates := make(chan netlink.LinkUpdate, 64)
	done := make(chan struct{})
	if err := netlink.LinkSubscribeWithOptions(updates, done, netlink.LinkSubscribeOptions{
		ErrorCallback: func(err error) {
			klog.V(1).Infof("Link subscription error for %s: %v", ifName, err)
		},
	}); err != nil {
		close(done)
		return nil, fmt.Errorf("failed to subscribe to link updates for %s: %w", ifName, err)
	}
	nl := &netlinkLink{
		ifName:   ifName,
		index:    l.Attrs().Index,
		events:   make(chan LinkObservation, 1),
		done:     done,
		finished: make(chan struct{}),
	}
	go nl.watch(updates)
	return nl, nil
}

func (l *netlinkLink) watch(updates <-chan netlink.LinkUpdate) {
	defer close(l.finished)
	defer close(l.events)
	for u := range updates {
		if int(u.Index) != l.index || u.Header.Type == unix.RTM_DELLINK {
			continue
		}
		publishLatest(l.events, observationFromFlags(u.Flags))
	}
}

// publishLatest sends obs on a channel of capacity 1, replacing an unread
// value. It must only be called by the channel's single sender.
func publishLatest[T any](ch chan T, v T) {
	select {
	case <-ch:
	default:
	}
	select {
	case ch <- v:
	default:
	}
}

func (l *netlinkLink) link() (netlink.Link, error) {
	nl, err := netlink.LinkByIndex(l.index)
	if err != nil {
		return nil, fmt.Errorf("interface %s (index %d) not found: %w", l.ifName, l.index, err)
	}
	return nl, nil
}

// SetAdminUp implements Link.
func (l *netlinkLink) SetAdminUp(up bool) error {
	nl, err := l.link()
	if err != nil {
		return err
	}
	if up {
		err = netlink.LinkSetUp(nl)
	} else {
		err = netlink.LinkSetDown(nl)
	}
	if err != nil {
		return fmt.Errorf("failed to set %s admin up=%t: %w", l.ifName, up, err)
	}
	return nil
}

// Current implements Link.
func (l *netlinkLink) Current() (LinkObservation, error) {
	nl, err := l.link()
	if err != nil {
		return LinkObservation{}, err
	}
	return observationFromFlags(nl.Attrs().RawFlags), nil
}

// Events implements Link.
func (l *netlinkLink) Events() <-chan LinkObservation {
	return l.events
}

// Close implements Link.
func (l *netlinkLink) Close() error {
	l.closeOnce.Do(func() { close(l.done) })
	<-l.finished
	return nil
}

// linkReporter decides which link state a bridge reports to its peer for a
// sequence of observations of its local interface.
//
// The reported state is the carrier of the interface: UP when it has carrier,
// DOWN when it does not. Nothing is reported while the interface is
// administratively down, because the bridge itself took it down to propagate
// the peer's state and the resulting loss of carrier is not news to the peer;
// reporting it would latch both ends down. The first observation after the
// interface comes back up is always reported, so that the peer learns the
// state of the far device.
type linkReporter struct {
	reported wpb.LinkState
}

// observe returns the state to report for o, or LINK_STATE_UNKNOWN if nothing
// should be reported.
func (r *linkReporter) observe(o LinkObservation) wpb.LinkState {
	if !o.AdminUp {
		r.reported = wpb.LinkState_LINK_STATE_UNKNOWN
		return wpb.LinkState_LINK_STATE_UNKNOWN
	}
	s := wpb.LinkState_LINK_STATE_DOWN
	if o.Carrier {
		s = wpb.LinkState_LINK_STATE_UP
	}
	if s == r.reported {
		return wpb.LinkState_LINK_STATE_UNKNOWN
	}
	r.reported = s
	return s
}

// current returns the last reported state, or LINK_STATE_UNKNOWN if the
// interface is administratively down or nothing was reported yet.
func (r *linkReporter) current() wpb.LinkState {
	return r.reported
}
