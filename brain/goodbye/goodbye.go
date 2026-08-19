// Package goodbye mengelola fitur pesan perpisahan otomatis ketika ada member yang keluar / di-kick dari grup WhatsApp.
package goodbye

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
	goodbyeChatKeyPrefix = "goodbye:chat:"
	goodbyeMsgKeyPrefix  = "goodbye:msg:"
	defaultTemplate      = "Selamat tinggal @user 👋\nTerima kasih telah menjadi bagian dari grup *{group}*.\nSemoga sukses dan sampai jumpa di lain kesempatan! 🌟"
)

// DefaultTemplate mengembalikan format pesan perpisahan bawaan.
func DefaultTemplate() string {
	return defaultTemplate
}

// IsEnabled mengecek apakah fitur goodbye aktif di grup tertentu.
func IsEnabled(groupID string) bool {
	var enabled bool
	found, err := store.Get(goodbyeChatKeyPrefix+groupID, &enabled)
	if err == nil && found {
		return enabled
	}
	return false
}

// SetEnabled mengubah status aktif (ON/OFF) goodbye di grup tertentu.
func SetEnabled(groupID string, enabled bool) error {
	return store.Set(goodbyeChatKeyPrefix+groupID, enabled)
}

// GetTemplate mengambil template pesan goodbye untuk grup tertentu.
func GetTemplate(groupID string) string {
	var custom string
	found, err := store.Get(goodbyeMsgKeyPrefix+groupID, &custom)
	if err == nil && found && strings.TrimSpace(custom) != "" {
		return strings.TrimSpace(custom)
	}
	return defaultTemplate
}

// SetTemplate menyimpan template pesan goodbye kustom untuk grup tertentu.
func SetTemplate(groupID string, template string) error {
	return store.Set(goodbyeMsgKeyPrefix+groupID, strings.TrimSpace(template))
}

// ResetTemplate menghapus template kustom dan kembali ke format bawaan.
func ResetTemplate(groupID string) error {
	return store.Delete(goodbyeMsgKeyPrefix + groupID)
}

// HasCustomTemplate mengembalikan true bila grup memiliki template kustom.
func HasCustomTemplate(groupID string) bool {
	var custom string
	found, err := store.Get(goodbyeMsgKeyPrefix+groupID, &custom)
	return err == nil && found && strings.TrimSpace(custom) != ""
}

// HandleGroupLeave memproses event ketika ada member yang keluar / dikeluarkan dari grup.
func HandleGroupLeave(ctx context.Context, client *whatsmeow.Client, evt *events.GroupInfo) {
	if client == nil || evt == nil || len(evt.Leave) == 0 {
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

	for _, userJID := range evt.Leave {
		// Jangan kirim ucapan perpisahan jika nomor bot sendiri yang keluar/di-kick
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
			log.Printf("[rbot] [goodbye] gagal kirim pesan perpisahan ke %s: %v", userJID.User, err)
		} else {
			log.Printf("[rbot] [goodbye] sukses kirim perpisahan member keluar %s di %s", userJID.User, groupName)
		}
	}
}

// GroupGoodbyeData adalah struktur data status goodbye untuk Web Dashboard.
type GroupGoodbyeData struct {
	JID            string `json:"jid"`
	Name           string `json:"name"`
	Enabled        bool   `json:"enabled"`
	Template       string `json:"template"`
	HasCustomMsg   bool   `json:"has_custom_msg"`
	ParticipantCnt int    `json:"participant_count"`
	BotType        string `json:"botType"`
	BotPhone       string `json:"botPhone"`
	BotLabel       string `json:"botLabel"`
}

// GetGroupsGoodbyeData mengambil daftar seluruh grup yang diikuti bot dan status goodbye-nya.
func GetGroupsGoodbyeData(ctx context.Context, client *whatsmeow.Client) ([]GroupGoodbyeData, error) {
	if client == nil || !client.IsConnected() || !client.IsLoggedIn() {
		return nil, fmt.Errorf("bot belum terhubung ke WhatsApp")
	}

	groups, err := client.GetJoinedGroups(ctx)
	if err != nil {
		return nil, fmt.Errorf("ambil daftar grup: %w", err)
	}

	results := make([]GroupGoodbyeData, 0, len(groups))
	for _, g := range groups {
		jidStr := g.JID.String()
		name := g.Name
		if name == "" {
			name = "Grup Tanpa Nama"
		}
		results = append(results, GroupGoodbyeData{
			JID:            jidStr,
			Name:           name,
			Enabled:        IsEnabled(jidStr),
			Template:       GetTemplate(jidStr),
			HasCustomMsg:   HasCustomTemplate(jidStr),
			ParticipantCnt: len(g.Participants),
			BotType:        "main",
			BotPhone:       "main",
			BotLabel:       "🤖 Bot Utama",
		})
	}

	return results, nil
}
