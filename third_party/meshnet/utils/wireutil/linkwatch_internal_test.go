package wireutil

import (
	"testing"

	"github.com/google/go-cmp/cmp"
	"github.com/vishvananda/netlink"
	"github.com/vishvananda/netlink/nl"
	"golang.org/x/sys/unix"
)

func linkUpdate(msgType uint16, index int32, name string, flags uint32) netlink.LinkUpdate {
	return netlink.LinkUpdate{
		IfInfomsg: nl.IfInfomsg{IfInfomsg: unix.IfInfomsg{Index: index, Flags: flags}},
		Header:    unix.NlMsghdr{Type: msgType},
		Link:      &netlink.Dummy{LinkAttrs: netlink.LinkAttrs{Name: name, Index: int(index)}},
	}
}

type adminChange struct {
	Name    string
	AdminUp bool
}

func TestWatchAdminUpdates(t *testing.T) {
	const (
		up      = unix.IFF_UP
		running = unix.IFF_UP | unix.IFF_LOWER_UP | unix.IFF_RUNNING
	)
	tests := []struct {
		desc    string
		updates []netlink.LinkUpdate
		want    []adminChange
	}{{
		desc: "existing links are reported once",
		updates: []netlink.LinkUpdate{
			linkUpdate(unix.RTM_NEWLINK, 1, "lo", up),
			linkUpdate(unix.RTM_NEWLINK, 2, "eth1", 0),
		},
		want: []adminChange{{"lo", true}, {"eth1", false}},
	}, {
		desc: "carrier changes are not reported",
		updates: []netlink.LinkUpdate{
			linkUpdate(unix.RTM_NEWLINK, 2, "eth1", up),
			linkUpdate(unix.RTM_NEWLINK, 2, "eth1", running),
			linkUpdate(unix.RTM_NEWLINK, 2, "eth1", up),
		},
		want: []adminChange{{"eth1", true}},
	}, {
		desc: "admin transitions are reported",
		updates: []netlink.LinkUpdate{
			linkUpdate(unix.RTM_NEWLINK, 2, "eth1", running),
			linkUpdate(unix.RTM_NEWLINK, 2, "eth1", 0),
			linkUpdate(unix.RTM_NEWLINK, 2, "eth1", 0),
			linkUpdate(unix.RTM_NEWLINK, 2, "eth1", up),
		},
		want: []adminChange{{"eth1", true}, {"eth1", false}, {"eth1", true}},
	}, {
		desc: "a rename is reported under the new name",
		updates: []netlink.LinkUpdate{
			linkUpdate(unix.RTM_NEWLINK, 2, "tmp0", up),
			linkUpdate(unix.RTM_NEWLINK, 2, "eth1", up),
		},
		want: []adminChange{{"tmp0", true}, {"eth1", true}},
	}, {
		desc: "a re-created link is reported again",
		updates: []netlink.LinkUpdate{
			linkUpdate(unix.RTM_NEWLINK, 2, "eth1", up),
			linkUpdate(unix.RTM_DELLINK, 2, "eth1", up),
			linkUpdate(unix.RTM_NEWLINK, 2, "eth1", up),
		},
		want: []adminChange{{"eth1", true}, {"eth1", true}},
	}}
	for _, tt := range tests {
		t.Run(tt.desc, func(t *testing.T) {
			ch := make(chan netlink.LinkUpdate, len(tt.updates))
			for _, u := range tt.updates {
				ch <- u
			}
			close(ch)
			var got []adminChange
			watchAdminUpdates(ch, func(name string, adminUp bool) {
				got = append(got, adminChange{name, adminUp})
			})
			if diff := cmp.Diff(tt.want, got); diff != "" {
				t.Errorf("watchAdminUpdates() changes diff (-want +got):\n%s", diff)
			}
		})
	}
}
