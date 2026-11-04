package wireutil

import (
	"fmt"
	"sync"

	log "github.com/sirupsen/logrus"
	"github.com/vishvananda/netlink"
	"github.com/vishvananda/netns"
	"golang.org/x/sys/unix"
)

// AdminStateFunc is called with the name of an interface and whether it is
// administratively up (IFF_UP).
type AdminStateFunc func(ifName string, adminUp bool)

// WatchAdminState reports the admin state (IFF_UP) of the interfaces in the
// network namespace at nsPath. onChange is called once for every interface
// that exists when the watch starts, and then whenever an interface appears or
// its admin state changes. Carrier (IFF_LOWER_UP) changes are deliberately not
// reported, so that carrier set on an interface never echoes back as an admin
// change. onChange is called from a single goroutine.
//
// The watch ends when stop is called or when the namespace goes away, after
// which onChange is no longer called. stop waits for the watch to end and may
// be called more than once.
func WatchAdminState(nsPath string, onChange AdminStateFunc) (stop func(), err error) {
	nsh, err := netns.GetFromPath(nsPath)
	if err != nil {
		return nil, fmt.Errorf("could not open netns %s: %w", nsPath, err)
	}
	// The subscription socket is created inside the namespace while
	// subscribing, so the handle is not needed afterwards.
	defer func() { _ = nsh.Close() }()

	updates := make(chan netlink.LinkUpdate, 64)
	done := make(chan struct{})
	if err := netlink.LinkSubscribeWithOptions(updates, done, netlink.LinkSubscribeOptions{
		Namespace:    &nsh,
		ListExisting: true,
		ErrorCallback: func(err error) {
			log.Debugf("WatchAdminState(%s): netlink subscription error: %v", nsPath, err)
		},
	}); err != nil {
		close(done)
		return nil, fmt.Errorf("could not subscribe to links in netns %s: %w", nsPath, err)
	}

	finished := make(chan struct{})
	go func() {
		defer close(finished)
		watchAdminUpdates(updates, onChange)
	}()

	var once sync.Once
	return func() {
		once.Do(func() { close(done) })
		<-finished
	}, nil
}

// watchAdminUpdates calls onChange for admin state transitions in updates
// until updates is closed.
func watchAdminUpdates(updates <-chan netlink.LinkUpdate, onChange AdminStateFunc) {
	type linkState struct {
		name    string
		adminUp bool
	}
	links := map[int32]linkState{}
	for u := range updates {
		idx := u.Index
		if u.Header.Type == unix.RTM_DELLINK {
			delete(links, idx)
			continue
		}
		cur := linkState{
			name:    u.Attrs().Name,
			adminUp: u.Flags&unix.IFF_UP != 0,
		}
		if prev, ok := links[idx]; ok && prev == cur {
			continue
		}
		links[idx] = cur
		onChange(cur.name, cur.adminUp)
	}
}
