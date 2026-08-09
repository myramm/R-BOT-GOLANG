// economy.go: helper bersama command ekonomi (resolve target user, cocokkan
// argumen, format tanggal). Dipakai addprem.go & addenergi.go. Port dari bagian
// atas premium.js/addprem.js/addenergi.js di bot Node lama.
package cmdfunc

import (
	"regexp"
	"time"

	"rbot/brain/command"
	"rbot/brain/config"
)

var (
	ReDigits       = regexp.MustCompile(`^\d+$`)
	ReSignedDigits = regexp.MustCompile(`^-?\d+$`)
	ReAll          = regexp.MustCompile(`(?i)^--?all$`)
	reNonDigit     = regexp.MustCompile(`[^0-9]`)
)

// ResolveTargetID meniru resolveTarget Node: mention pertama → participant pesan
// yang di-reply → argumen angka (≥6 digit). Kosong bila tak ada target.
func ResolveTargetID(c *command.Ctx) string {
	if ci := c.ContextInfo(); ci != nil {
		if m := ci.GetMentionedJID(); len(m) > 0 {
			if id := config.BareNumber(m[0]); id != "" {
				return id
			}
		}
		if id := config.BareNumber(ci.GetParticipant()); id != "" {
			return id
		}
	}
	for _, a := range c.Args {
		if d := reNonDigit.ReplaceAllString(a, ""); len(d) >= 6 {
			return d
		}
	}
	return ""
}

// FirstArgMatch mengembalikan argumen pertama yang cocok regex, atau "".
func FirstArgMatch(args []string, re *regexp.Regexp) string {
	for _, a := range args {
		if re.MatchString(a) {
			return a
		}
	}
	return ""
}

// FmtDate: timestamp ms → DD/MM/YYYY waktu lokal (port fmtDate addprem.js).
func FmtDate(ms int64) string { return time.UnixMilli(ms).Format("02/01/2006") }
