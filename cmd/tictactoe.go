package cmd

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"time"

	"rbot/brain/command"
	"rbot/lib/richmessage"
)

const (
	ttBoardSize  = 9
	ttPlayer     = "⭕"
	ttBot        = "❌"
	ttEmpty      = "⬜"
	ttExpiryTime = 10 * time.Minute
)

type tictactoeGame struct {
	board      [ttBoardSize]string
	turn       string
	playerJID  string
	chatJID    string
	startedAt  time.Time
	finishedAt time.Time
	winner     string
}

var (
	ttMu    sync.Mutex
	ttGames = map[string]*tictactoeGame{}
)

func init() {
	command.Register(&command.Command{
		Name:        "tictactoe",
		Category:    "Game",
		Alias:       []string{"ttt", "tic"},
		Description: "Main Tic-Tac-Toe lawan bot via rich message. Ketik .ttt untuk mulai, .ttt <1-9> untuk isi cell, .ttt quit untuk menyerah.",
		Handler:     tictactoeHandler,
	})
}

func tictactoeHandler(ctx context.Context, c *command.Ctx) error {
	ttCleanup()
	key := c.Evt.Info.Sender.String()
	chatJID := c.Evt.Info.Chat.String()

	if len(c.Args) == 0 || ttEqualFold(c.Args[0], "start") {
		return ttStart(ctx, c, key, chatJID)
	}
	if ttEqualFold(c.Args[0], "quit") || ttEqualFold(c.Args[0], "stop") {
		return ttQuit(ctx, c, key, chatJID)
	}
	if ttEqualFold(c.Args[0], "board") {
		return ttShowBoard(ctx, c, key, chatJID)
	}

	cell, err := ttParseCell(c.Args[0])
	if err != nil {
		_, e := c.Reply(ctx, "⚠️ Argumen tidak valid. Ketik *.ttt* untuk mulai, *.ttt 1-9* untuk isi cell, *.ttt quit* untuk menyerah.")
		return e
	}
	return ttMove(ctx, c, key, chatJID, cell)
}

func ttStart(ctx context.Context, c *command.Ctx, key, chatJID string) error {
	ttMu.Lock()
	if g, ada := ttGames[key]; ada && !g.finishedAt.IsZero() {
		delete(ttGames, key)
	}
	if _, ada := ttGames[key]; ada {
		ttMu.Unlock()
		_, e := c.Reply(ctx, "⚠️ Kamu masih punya permainan yang sedang jalan. Pilih cell dari tombol, atau *.ttt quit* untuk menyerah.")
		return e
	}
	g := &tictactoeGame{
		board:     [ttBoardSize]string{},
		turn:      "player",
		playerJID: c.Evt.Info.Sender.String(),
		chatJID:   chatJID,
		startedAt: time.Now(),
	}
	ttGames[key] = g
	ttMu.Unlock()
	return ttRenderBoard(ctx, c, g, "🎮 *Tic-Tac-Toe dimulai!*\n\nGiliran kamu (⭕). Pilih cell dari tombol atau ketik *.ttt <1-9>*.")
}

func ttQuit(ctx context.Context, c *command.Ctx, key, chatJID string) error {
	ttMu.Lock()
	_, ada := ttGames[key]
	if !ada {
		ttMu.Unlock()
		_, e := c.Reply(ctx, "Tidak ada permainan yang sedang berjalan. Ketik *.ttt* untuk mulai.")
		return e
	}
	delete(ttGames, key)
	ttMu.Unlock()
	_, e := c.Reply(ctx, "🏳️ Permainan dihentikan. Ketik *.ttt* untuk main lagi.")
	return e
}

func ttShowBoard(ctx context.Context, c *command.Ctx, key, chatJID string) error {
	ttMu.Lock()
	g, ada := ttGames[key]
	ttMu.Unlock()
	if !ada {
		_, e := c.Reply(ctx, "Tidak ada permainan aktif. Ketik *.ttt* untuk mulai.")
		return e
	}
	return ttRenderBoard(ctx, c, g, "")
}

func ttMove(ctx context.Context, c *command.Ctx, key, chatJID string, cell int) error {
	ttMu.Lock()
	g, ada := ttGames[key]
	if !ada {
		ttMu.Unlock()
		_, e := c.Reply(ctx, "Tidak ada permainan aktif. Ketik *.ttt* untuk mulai.")
		return e
	}
	if !g.finishedAt.IsZero() {
		ttMu.Unlock()
		_, e := c.Reply(ctx, "🏁 Permainan sudah selesai. Ketik *.ttt* untuk main lagi.")
		return e
	}
	if g.turn != "player" {
		ttMu.Unlock()
		_, e := c.Reply(ctx, "⏳ Tunggu bot selesai berpikir dulu...")
		return e
	}
	if g.board[cell] != "" {
		ttMu.Unlock()
		_, e := c.Reply(ctx, fmt.Sprintf("❌ Cell *%d* sudah terisi. Pilih cell lain.", cell+1))
		return e
	}

	g.board[cell] = ttPlayer
	if winner := ttCheckWinner(g.board); winner != "" {
		g.finishedAt = time.Now()
		g.winner = winner
		ttMu.Unlock()
		return ttRenderBoard(ctx, c, g, "🎉 *Kamu MENANG!* 🎉\n\nMain lagi? Ketik *.ttt*")
	}
	if ttIsFull(g.board) {
		g.finishedAt = time.Now()
		g.winner = "draw"
		ttMu.Unlock()
		return ttRenderBoard(ctx, c, g, "🤝 *SERI!* Coba lagi? Ketik *.ttt*")
	}

	g.turn = "bot"
	botCell := ttChooseBotMove(g.board)
	if botCell >= 0 {
		g.board[botCell] = ttBot
	}
	if winner := ttCheckWinner(g.board); winner != "" {
		g.finishedAt = time.Now()
		g.winner = winner
		ttMu.Unlock()
		return ttRenderBoard(ctx, c, g, "🤖 *Bot MENANG!* Coba lagi? Ketik *.ttt*")
	}
	if ttIsFull(g.board) {
		g.finishedAt = time.Now()
		g.winner = "draw"
		ttMu.Unlock()
		return ttRenderBoard(ctx, c, g, "🤝 *SERI!* Coba lagi? Ketik *.ttt*")
	}
	g.turn = "player"
	ttMu.Unlock()
	return ttRenderBoard(ctx, c, g, fmt.Sprintf("Bot memilih cell *%d*. Giliran kamu (⭕).", botCell+1))
}

func ttEqualFold(a, b string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := 0; i < len(a); i++ {
		ca := a[i]
		if ca >= 'A' && ca <= 'Z' {
			ca += 'a' - 'A'
		}
		cb := b[i]
		if cb >= 'A' && cb <= 'Z' {
			cb += 'a' - 'A'
		}
		if ca != cb {
			return false
		}
	}
	return true
}

func ttParseCell(s string) (int, error) {
	if len(s) != 1 {
		return 0, fmt.Errorf("invalid cell")
	}
	if s[0] < '1' || s[0] > '9' {
		return 0, fmt.Errorf("invalid cell")
	}
	return int(s[0] - '1'), nil
}

func ttIsFull(b [ttBoardSize]string) bool {
	for _, c := range b {
		if c == "" {
			return false
		}
	}
	return true
}

func ttCheckWinner(b [ttBoardSize]string) string {
	wins := [][3]int{
		{0, 1, 2}, {3, 4, 5}, {6, 7, 8},
		{0, 3, 6}, {1, 4, 7}, {2, 5, 8},
		{0, 4, 8}, {2, 4, 6},
	}
	for _, w := range wins {
		if b[w[0]] != "" && b[w[0]] == b[w[1]] && b[w[0]] == b[w[2]] {
			if b[w[0]] == ttPlayer {
				return "player"
			}
			return "bot"
		}
	}
	return ""
}

func ttChooseBotMove(b [ttBoardSize]string) int {
	if c := ttFindWinningMove(b, ttBot); c >= 0 {
		return c
	}
	if c := ttFindWinningMove(b, ttPlayer); c >= 0 {
		return c
	}
	if b[4] == "" {
		return 4
	}
	corners := []int{0, 2, 6, 8}
	for _, c := range corners {
		if b[c] == "" {
			return c
		}
	}
	edges := []int{1, 3, 5, 7}
	for _, c := range edges {
		if b[c] == "" {
			return c
		}
	}
	return -1
}

func ttFindWinningMove(b [ttBoardSize]string, mark string) int {
	wins := [][3]int{
		{0, 1, 2}, {3, 4, 5}, {6, 7, 8},
		{0, 3, 6}, {1, 4, 7}, {2, 5, 8},
		{0, 4, 8}, {2, 4, 6},
	}
	for _, w := range wins {
		own, empty := 0, -1
		for _, idx := range w {
			if b[idx] == mark {
				own++
			} else if b[idx] == "" {
				empty = idx
			}
		}
		if own == 2 && empty >= 0 {
			return empty
		}
	}
	return -1
}

func ttCleanup() {
	ttMu.Lock()
	defer ttMu.Unlock()
	now := time.Now()
	for k, g := range ttGames {
		if g.finishedAt.IsZero() && now.Sub(g.startedAt) > ttExpiryTime {
			delete(ttGames, k)
		}
		if !g.finishedAt.IsZero() && now.Sub(g.finishedAt) > ttExpiryTime {
			delete(ttGames, k)
		}
	}
}

func ttRenderBoard(ctx context.Context, c *command.Ctx, g *tictactoeGame, footer string) error {
	board := g.board

	boardText := fmt.Sprintf(
		"*%s* │ *%s* │ *%s*\n"+
			"────┼────┼────\n"+
			"*%s* │ *%s* │ *%s*\n"+
			"────┼────┼────\n"+
			"*%s* │ *%s* │ *%s*",
		ttCellText(board[0], 0), ttCellText(board[1], 1), ttCellText(board[2], 2),
		ttCellText(board[3], 3), ttCellText(board[4], 4), ttCellText(board[5], 5),
		ttCellText(board[6], 6), ttCellText(board[7], 7), ttCellText(board[8], 8),
	)

	turnInfo := ""
	if g.finishedAt.IsZero() {
		if g.turn == "player" {
			turnInfo = "Giliran: ⭕ Kamu"
		} else {
			turnInfo = "Giliran: ❌ Bot (berpikir...)"
		}
	}

	body := turnInfo + "\n\n" + boardText
	if footer != "" {
		body += "\n\n" + footer
	}

	xml := ttBuildRichXML(body, footer, g)
	rm, err := richmessage.Parse(xml)
	if err != nil {
		_, _ = c.Reply(ctx, body)
		return nil
	}

	msg := rm.ToProtoMessage(".ttt ")
	_, err = c.Client.SendMessage(ctx, c.Evt.Info.Chat, msg)
	if err != nil {
		plain := "🎮 *Tic-Tac-Toe*\n\n" + body
		_, fallbackErr := c.Reply(ctx, plain)
		if fallbackErr != nil {
			return fallbackErr
		}
	}
	return err
}

func ttCellText(cell string, idx int) string {
	if cell == "" {
		return fmt.Sprintf("%d", idx+1)
	}
	return cell
}

func ttBuildRichXML(body, footer string, g *tictactoeGame) string {
	var xml string
	xml = "<rich>\n"
	xml += "\t<header>🎮 Tic-Tac-Toe</header>\n"
	xml += "\t<body>" + escapeXML(body) + "</body>\n"
	if footer != "" {
		xml += "\t<footer>" + escapeXML(footer) + "</footer>\n"
	}
	if !g.finishedAt.IsZero() {
		xml += `\t<button id="start">🔄 Main lagi</button>` + "\n"
		xml += `\t<button id="quit">🏳️ Berhenti</button>` + "\n"
	} else if g.turn == "player" {
		available := []int{}
		for i, c := range g.board {
			if c == "" {
				available = append(available, i)
			}
		}
		for i := 0; i < len(available) && i < 9; i++ {
			idx := available[i]
			id := fmt.Sprintf("%d", idx+1)
			xml += fmt.Sprintf(`\t<button id="%s">%s</button>`+"\n", id, escapeXML(ttEmpty+" "+id))
		}
		if len(available) > 0 {
			xml += `\t<button id="quit">🏳️ Menyerah</button>` + "\n"
		}
	} else {
		xml += `\t<button id="board">⏳ Lihat papan</button>` + "\n"
	}
	xml += "</rich>"
	return xml
}

func escapeXML(s string) string {
	r := strings.NewReplacer("&", "&amp;", "<", "&lt;", ">", "&gt;", "\"", "&quot;")
	return r.Replace(s)
}
