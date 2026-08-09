// Package stats menyimpan statistik chat dan command bot.
// Skemanya kompatibel dengan chatStats Node: users dan groups dipisah, dengan
// hitungan chats/cmds per user serta ranking command per grup.
package stats

import (
	"sort"
	"sync"
	"time"

	"go.mau.fi/whatsmeow/types"
	"go.mau.fi/whatsmeow/types/events"

	"rbot/brain/command"
	"rbot/brain/config"
	"rbot/brain/store"
)

const storeKey = "chatStats"

type Counter struct {
	Chats int64 `json:"chats"`
	Cmds  int64 `json:"cmds"`
}

type groupStats struct {
	Cmds    int64              `json:"cmds"`
	Members map[string]*Counter `json:"members"`
}

type state struct {
	Users  map[string]*Counter    `json:"users"`
	Groups map[string]*groupStats `json:"groups"`
}

var (
	mu           sync.Mutex
	cached       *state
	loaded       bool
	dirty        bool
	lastPersist  time.Time
)

const persistInterval = 5 * time.Second

func emptyState() *state {
	return &state{Users: map[string]*Counter{}, Groups: map[string]*groupStats{}}
}

func load() *state {
	if loaded && cached != nil {
		return cached
	}
	s := emptyState()
	_, _ = store.Get(storeKey, s)
	if s.Users == nil {
		s.Users = map[string]*Counter{}
	}
	if s.Groups == nil {
		s.Groups = map[string]*groupStats{}
	}
	for _, g := range s.Groups {
		if g.Members == nil {
			g.Members = map[string]*Counter{}
		}
	}
	cached = s
	loaded = true
	return s
}

func save(s *state) {
	cached = s
	loaded = true
	dirty = true
	if lastPersist.IsZero() || time.Since(lastPersist) >= persistInterval {
		_ = store.Set(storeKey, s)
		lastPersist = time.Now()
		dirty = false
	}
}

// Flush menyimpan perubahan statistik yang belum ditulis. Panggil sebelum store
// ditutup agar pesan terakhir tetap persisten.
func Flush() {
	mu.Lock()
	defer mu.Unlock()
	if loaded && dirty && cached != nil {
		_ = store.Set(storeKey, cached)
		lastPersist = time.Now()
		dirty = false
	}
}

func ensureUser(s *state, jid string) *Counter {
	if s.Users[jid] == nil {
		s.Users[jid] = &Counter{}
	}
	return s.Users[jid]
}

func ensureGroup(s *state, jid string) *groupStats {
	if s.Groups[jid] == nil {
		s.Groups[jid] = &groupStats{Members: map[string]*Counter{}}
	}
	if s.Groups[jid].Members == nil {
		s.Groups[jid].Members = map[string]*Counter{}
	}
	return s.Groups[jid]
}

func canonicalJID(jid types.JID) string {
	if jid.IsEmpty() {
		return ""
	}
	user := config.BareNumber(jid.User)
	if user == "" {
		return ""
	}
	return types.NewJID(user, jid.Server).String()
}

func senderKey(evt *events.Message) string {
	if evt == nil {
		return ""
	}
	if s := canonicalJID(evt.Info.Sender); s != "" {
		return s
	}
	return canonicalJID(evt.Info.Chat)
}

func groupKey(evt *events.Message) string {
	if evt == nil || !evt.Info.IsGroup {
		return ""
	}
	return canonicalJID(evt.Info.Chat)
}

// AddChat mencatat satu pesan masuk, termasuk pesan yang bukan command.
func AddChat(evt *events.Message) {
	jid := senderKey(evt)
	if jid == "" {
		return
	}
	mu.Lock()
	defer mu.Unlock()
	s := load()
	ensureUser(s, jid).Chats++
	if group := groupKey(evt); group != "" {
		g := ensureGroup(s, group)
		if g.Members[jid] == nil {
			g.Members[jid] = &Counter{}
		}
		g.Members[jid].Chats++
	}
	save(s)
}

// AddCmd mencatat satu command yang berhasil di-resolve.
func AddCmd(evt *events.Message) {
	jid := senderKey(evt)
	if jid == "" {
		return
	}
	mu.Lock()
	defer mu.Unlock()
	s := load()
	ensureUser(s, jid).Cmds++
	if group := groupKey(evt); group != "" {
		g := ensureGroup(s, group)
		g.Cmds++
		if g.Members[jid] == nil {
			g.Members[jid] = &Counter{}
		}
		g.Members[jid].Cmds++
	}
	save(s)
}

func init() { command.StatsHook = func(_ string, evt *events.Message) { AddCmd(evt) } }

type UserEntry struct {
	JID string
	Counter
}

type GroupEntry struct {
	JID  string
	Cmds int64
}

// UserStats mengembalikan hitungan global pengirim.
func UserStats(evt *events.Message) Counter {
	jid := senderKey(evt)
	mu.Lock()
	defer mu.Unlock()
	s := load()
	if c := s.Users[jid]; c != nil {
		return *c
	}
	return Counter{}
}

// TopGroupUsers mengembalikan ranking anggota grup berdasarkan chat lalu command.
func TopGroupUsers(evt *events.Message) []UserEntry {
	group := groupKey(evt)
	mu.Lock()
	defer mu.Unlock()
	s := load()
	g := s.Groups[group]
	if g == nil {
		return nil
	}
	out := make([]UserEntry, 0, len(g.Members))
	for jid, c := range g.Members {
		if c != nil {
			out = append(out, UserEntry{JID: jid, Counter: *c})
		}
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Chats != out[j].Chats {
			return out[i].Chats > out[j].Chats
		}
		return out[i].Cmds > out[j].Cmds
	})
	if len(out) > 30 {
		out = out[:30]
	}
	return out
}

// TopGroups mengembalikan grup berdasarkan jumlah command.
func TopGroups() []GroupEntry {
	mu.Lock()
	defer mu.Unlock()
	s := load()
	out := make([]GroupEntry, 0, len(s.Groups))
	for jid, g := range s.Groups {
		if g != nil {
			out = append(out, GroupEntry{JID: jid, Cmds: g.Cmds})
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Cmds > out[j].Cmds })
	if len(out) > 30 {
		out = out[:30]
	}
	return out
}

// TopUsers mengembalikan ranking user global berdasarkan chat lalu command.
func TopUsers() []UserEntry {
	mu.Lock()
	defer mu.Unlock()
	s := load()
	out := make([]UserEntry, 0, len(s.Users))
	for jid, c := range s.Users {
		if c != nil {
			out = append(out, UserEntry{JID: jid, Counter: *c})
		}
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Chats != out[j].Chats {
			return out[i].Chats > out[j].Chats
		}
		return out[i].Cmds > out[j].Cmds
	})
	if len(out) > 30 {
		out = out[:30]
	}
	return out
}
