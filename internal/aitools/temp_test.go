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
		{"replace_playlist_items", `{"id":"temp1","uris":[]}`, true},   // temp -> skip confirm
		{"remove_playlist_items", `{"id":"temp1"}`, true},              // temp -> skip
		{"unfollow_playlist", `{"id":"temp1"}`, true},                  // temp -> skip
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
