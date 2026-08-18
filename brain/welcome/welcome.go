// Package welcome mengelola fitur pesan sambutan otomatis ketika ada member baru masuk ke grup WhatsApp.
package welcome

import (
	"context"
	"fmt"
	"log"
	"strings"
	"time"

	"google.golang.org/protobuf/proto"

	"go.mau.fi/whatsmeow"
	waE2E "go.mau.fi/whatsmeow/proto/waE2E"
	"go.mau.fi/whatsmeow/types/events"

	"rbot/brain/store"
)

const (
	welcomeChatKeyPrefix = "welcome:chat:"
	welcomeMsgKeyPrefix  = "welcome:msg:"
	defaultTemplate      = "Halo @user 👋\nSelamat datang di grup *{group}*!\nSemoga betah dan jangan lupa baca deskripsi/aturan grup ya ✨"
)

// DefaultTemplate mengembalikan format pesan sambutan bawaan.
func DefaultTemplate() string {
	return defaultTemplate
}

// IsEnabled mengecek apakah fitur welcome aktif di grup tertentu.
func IsEnabled(groupID string) bool {
	var enabled bool
	found, err := store.Get(welcomeChatKeyPrefix+groupID, &enabled)
	if err == nil && found {
		return enabled
	}
	return false
}

// SetEnabled mengubah status aktif (ON/OFF) welcome di grup tertentu.
func SetEnabled(groupID string, enabled bool) error {
	return store.Set(welcomeChatKeyPrefix+groupID, enabled)
}

// GetTemplate mengambil template pesan welcome untuk grup tertentu.
func GetTemplate(groupID string) string {
	var custom string
	found, err := store.Get(welcomeMsgKeyPrefix+groupID, &custom)
	if err == nil && found && strings.TrimSpace(custom) != "" {
		return strings.TrimSpace(custom)
	}
	return defaultTemplate
}

// SetTemplate menyimpan template pesan welcome kustom untuk grup tertentu.
func SetTemplate(groupID string, template string) error {
	return store.Set(welcomeMsgKeyPrefix+groupID, strings.TrimSpace(template))
}

// ResetTemplate menghapus template kustom dan kembali ke format bawaan.
func ResetTemplate(groupID string) error {
	return store.Delete(welcomeMsgKeyPrefix + groupID)
}

// HasCustomTemplate mengembalikan true bila grup memiliki template kustom.
func HasCustomTemplate(groupID string) bool {
	var custom string
	found, err := store.Get(welcomeMsgKeyPrefix+groupID, &custom)
	return err == nil && found && strings.TrimSpace(custom) != ""
}

// HandleGroupJoin memproses event ketika ada member baru bergabung ke grup.
func HandleGroupJoin(ctx context.Context, client *whatsmeow.Client, evt *events.GroupInfo) {
	if client == nil || evt == nil || len(evt.Join) == 0 {
		return
	}

	groupID := evt.JID.String()
	if !IsEnabled(groupID) {
		return
	}

	groupName := "Grup"
	groupDesc := ""
	if evt.Name != nil && evt.Name.Name != "" {
		groupName = evt.Name.Name
	} else {
		// Ambil metadata grup
		infoCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
		if info, err := client.GetGroupInfo(infoCtx, evt.JID); err == nil && info != nil {
			if strings.TrimSpace(info.Name) != "" {
				groupName = info.Name
			}
			groupDesc = info.Topic
		}
		cancel()
	}

	template := GetTemplate(groupID)

	for _, userJID := range evt.Join {
		// Jangan sambut nomor bot sendiri jika baru masuk
		if client.Store != nil {
			if client.Store.ID != nil && userJID.User == client.Store.ID.User {
				continue
			}
			if jid := client.Store.GetJID(); !jid.IsEmpty() && userJID.User == jid.User {
				continue
			}
			if lid := client.Store.GetLID(); !lid.IsEmpty() && userJID.User == lid.User {
				continue
			}
		}

		userMention := "@" + userJID.User
		msgText := template
		msgText = strings.ReplaceAll(msgText, "{user}", userMention)
		msgText = strings.ReplaceAll(msgText, "@user", userMention)
		msgText = strings.ReplaceAll(msgText, "{group}", groupName)
		msgText = strings.ReplaceAll(msgText, "{desc}", groupDesc)

		mentionedJIDs := []string{userJID.String()}

		msg := &waE2E.Message{
			ExtendedTextMessage: &waE2E.ExtendedTextMessage{
				Text: proto.String(msgText),
				ContextInfo: &waE2E.ContextInfo{
					MentionedJID: mentionedJIDs,
				},
			},
		}

		sendCtx, cancel := context.WithTimeout(ctx, 15*time.Second)
		_, err := client.SendMessage(sendCtx, evt.JID, msg)
		cancel()
		if err != nil {
			log.Printf("[rbot] [welcome] gagal kirim pesan sambutan ke %s: %v", userJID.User, err)
		} else {
			log.Printf("[rbot] [welcome] sukses kirim sambutan member baru %s di %s", userJID.User, groupName)
		}
	}
}

// GroupWelcomeData adalah struktur data status welcome untuk Web Dashboard.
type GroupWelcomeData struct {
	JID            string `json:"jid"`
	Name           string `json:"name"`
	Enabled        bool   `json:"enabled"`
	Template       string `json:"template"`
	HasCustomMsg   bool   `json:"has_custom_msg"`
	ParticipantCnt int    `json:"participant_count"`
}

// GetGroupsWelcomeData mengambil daftar seluruh grup yang diikuti bot dan status welcome-nya.
func GetGroupsWelcomeData(ctx context.Context, client *whatsmeow.Client) ([]GroupWelcomeData, error) {
	if client == nil || !client.IsConnected() || !client.IsLoggedIn() {
		return nil, fmt.Errorf("bot belum terhubung ke WhatsApp")
	}

	groups, err := client.GetJoinedGroups(ctx)
	if err != nil {
		return nil, fmt.Errorf("ambil daftar grup: %w", err)
	}

	results := make([]GroupWelcomeData, 0, len(groups))
	for _, g := range groups {
		jidStr := g.JID.String()
		results = append(results, GroupWelcomeData{
			JID:            jidStr,
			Name:           g.Name,
			Enabled:        IsEnabled(jidStr),
			Template:       GetTemplate(jidStr),
			HasCustomMsg:   HasCustomTemplate(jidStr),
			ParticipantCnt: len(g.Participants),
		})
	}

	return results, nil
}
