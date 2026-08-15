package cmd

import (
	"context"
	"fmt"
	"math"
	"strings"
	"sync"
	"time"

	"rbot/brain/command"
	"rbot/brain/config"
	"rbot/brain/energy"
	"rbot/brain/identity"
	"rbot/brain/store"
)

const restStoreKey = "istirahat_cooldowns"

type restOption struct {
	ID       string
	Name     string
	Emoji    string
	Energy   int
	Cooldown time.Duration
}

var restOptions = []restOption{
	{ID: "nap", Name: "Tidur Siang", Emoji: "☕", Energy: 5, Cooldown: 1 * time.Hour},
	{ID: "tidur", Name: "Tidur Malam", Emoji: "🛌", Energy: 15, Cooldown: 4 * time.Hour},
	{ID: "nyenyak", Name: "Istirahat Total", Emoji: "💤", Energy: 30, Cooldown: 8 * time.Hour},
}

type restUserRecord map[string]int64

var restMu sync.Mutex

func getRestMap() map[string]restUserRecord {
	data := map[string]restUserRecord{}
	_, _ = store.Get(restStoreKey, &data)
	if data == nil {
		data = map[string]restUserRecord{}
	}
	return data
}

func saveRestMap(data map[string]restUserRecord) {
	_ = store.Set(restStoreKey, data)
}

func init() {
	command.Register(&command.Command{
		Name:        "istirahat",
		Category:    "Info",
		Alias:       []string{"rest", "tidur", "nap"},
		Description: "Istirahat untuk memulihkan energi bot",
		Handler:     istirahatHandler,
	})
}

func istirahatHandler(ctx context.Context, c *command.Ctx) error {
	mp := config.MainPrefix()
	e := energy.Get(c.Evt)

	if e.Unlimited {
		_, err := c.Reply(ctx, "⚡ Energi kamu sudah tak terbatas!")
		return err
	}

	sub := ""
	if len(c.Args) > 0 {
		sub = strings.ToLower(c.Args[0])
	}

	var sel *restOption
	if sub == "" {
		sel = &restOptions[0]
	} else {
		for i := range restOptions {
			if restOptions[i].ID == sub || strings.Contains(strings.ToLower(restOptions[i].Name), sub) {
				sel = &restOptions[i]
				break
			}
		}
	}

	if sel == nil {
		var list []string
		for _, opt := range restOptions {
			list = append(list, fmt.Sprintf("%s *%s%s %s* — +%d ⚡ (cooldown %v)", opt.Emoji, mp, c.InvokedAs, opt.ID, opt.Energy, opt.Cooldown))
		}
		_, err := c.Reply(ctx, fmt.Sprintf("💤 *Pilihan Istirahat:*\n\n%s\n\n_Contoh: *%s%s nap*_", strings.Join(list, "\n"), mp, c.InvokedAs))
		return err
	}

	candidates := identity.Candidates(c.Evt)
	if len(candidates) == 0 {
		_, err := c.Reply(ctx, "❌ Gagal mengidentifikasi user.")
		return err
	}
	userID := candidates[0]

	restMu.Lock()
	data := getRestMap()
	uRec := data[userID]
	if uRec == nil {
		uRec = restUserRecord{}
	}

	now := time.Now().UnixMilli()
	lastTS := uRec[sel.ID]
	sisa := time.Duration(lastTS+sel.Cooldown.Milliseconds()-now) * time.Millisecond

	if sisa > 0 {
		restMu.Unlock()
		minut := int(math.Ceil(sisa.Minutes()))
		_, err := c.Reply(ctx, fmt.Sprintf("⏳ Kamu masih kelelahan dari %s *%s*!\n\n_Tunggu *%d menit* lagi sebelum dapat istirahat ini._", sel.Emoji, sel.Name, minut))
		return err
	}

	res := energy.Restore(c.Evt, sel.Energy)
	if !res.OK {
		restMu.Unlock()
		_, err := c.Reply(ctx, "❌ "+res.Err)
		return err
	}

	if res.Penuh {
		restMu.Unlock()
		_, err := c.Reply(ctx, fmt.Sprintf("⚡ Energi kamu sudah penuh (*%d ⚡*)! Tidak perlu istirahat lagi saat ini.", res.Bank))
		return err
	}

	uRec[sel.ID] = now
	data[userID] = uRec
	saveRestMap(data)
	restMu.Unlock()

	_, err := c.Reply(ctx, fmt.Sprintf("%s *%s Berhasil!*\n\n"+
		"⚡ Energi bertambah: *+%d ⚡*\n"+
		"🔋 Total energi sekarang: *%d ⚡*\n\n"+
		"_Cooldown mode ini: %v_", sel.Emoji, sel.Name, res.Tambah, res.Bank, sel.Cooldown))
	return err
}
