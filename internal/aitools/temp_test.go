package aitools

import "testing"

type fakeTemp map[string]bool

func (f fakeTemp) Add(id string)      { f[id] = true }
func (f fakeTemp) Has(id string) bool { return f[id] }
func (f fakeTemp) Remove(id string)   { delete(f, id) }

func TestIsTempPlaylistEdit(t *testing.T) {
	tp := fakeTemp{"temp1": true}

	cases := []struct {
		name  string
		input string
		want  bool
	}{
		{"replace_playlist_items", `{"id":"temp1","uris":[]}`, true},   // queue edit -> skip confirm
		{"remove_playlist_items", `{"id":"temp1"}`, true},              // queue edit -> skip
		{"unfollow_playlist", `{"id":"temp1"}`, false},                 // deleting a queue still confirms
		{"replace_playlist_items", `{"id":"real99","uris":[]}`, false}, // real -> confirm
		{"remove_saved_tracks", `{"ids":["x"]}`, false},                // library op -> confirm
	}
	for _, c := range cases {
		if got := IsTempPlaylistEdit(tp, c.name, []byte(c.input)); got != c.want {
			t.Errorf("IsTempPlaylistEdit(%s, %s) = %v, want %v", c.name, c.input, got, c.want)
		}
	}
	if IsTempPlaylistEdit(nil, "replace_playlist_items", []byte(`{"id":"temp1"}`)) {
		t.Error("nil registry should never exempt")
	}
}

func TestAddedTrackURIs(t *testing.T) {
	cases := []struct {
		name, input string
		wantLen     int
	}{
		{"add_to_queue", `{"uri":"spotify:track:x"}`, 1},
		{"add_tracks_to_queue", `{"uris":["a","b","c"]}`, 3},
		{"add_items_to_playlist", `{"id":"p","uris":["a","b"]}`, 2},
		{"replace_playlist_items", `{"id":"p","uris":["a"]}`, 1},
		{"get_track", `{"id":"x"}`, 0}, // not an add tool
		{"remove_playlist_items", `{"id":"p","uris":["a"]}`, 0},
	}
	for _, c := range cases {
		if got := AddedTrackURIs(c.name, []byte(c.input)); len(got) != c.wantLen {
			t.Errorf("AddedTrackURIs(%s) len = %d, want %d", c.name, len(got), c.wantLen)
		}
	}
}
