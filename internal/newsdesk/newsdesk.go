package newsdesk

import (
	"context"
	"fmt"
	"log"
	"strings"
	"time"

	"github.com/barandras/gonewsdesk/pkg/news"
	"github.com/gdamore/tcell/v2"
	"github.com/rivo/tview"
)

const (
	regionalIndicatorA = rune(0x1F1E6)
	regionalIndicatorZ = rune(0x1F1FF)
)

type NewsDesk struct {
	ctx               context.Context
	NewsProcessor     *news.NewsProcessor
	app               *tview.Application
	highlightKeywords []string
	onShutdown        func()
	debug             bool
}

type NewNewsDeskParams struct {
	Ctx context.Context
	// NewsProcessor is the merged, filtered stream from pkg/news.
	NewsProcessor *news.NewsProcessor
	// HighlightKeywords: headlines matching any keyword get a yellow tile border.
	HighlightKeywords []string
	// OnShutdown is invoked when the user quits the UI (e.g. cancel the root context). Optional but recommended.
	OnShutdown func()
	// Debug: enable debug mode
	Debug bool
}

func NewNewsDesk(params NewNewsDeskParams) *NewsDesk {
	return &NewsDesk{
		ctx:               params.Ctx,
		NewsProcessor:     params.NewsProcessor,
		highlightKeywords: append([]string(nil), params.HighlightKeywords...),
		onShutdown:        params.OnShutdown,
		debug:             params.Debug,
	}
}

// Run builds the tview UI, subscribes to NewsProcessor.Stream(), and blocks until the app exits.
func (d *NewsDesk) Run() error {
	if d == nil {
		return fmt.Errorf("nil NewsDesk")
	}
	if d.NewsProcessor == nil {
		return fmt.Errorf("nil NewsProcessor")
	}
	if d.app != nil {
		return fmt.Errorf("Run already in progress")
	}

	app := tview.NewApplication()
	d.app = app

	go func() {
		<-d.ctx.Done()
		app.Stop()
	}()

	logBox := newLogBox(d.ctx)
	logCh := logBox.GetLogChAndStart()

	// Redirect the standard log package output to the log channel
	logWriter := &logChannelWriter{ch: logCh}
	log.SetOutput(logWriter)
	log.SetFlags(log.Ldate | log.Ltime | log.Lshortfile) // Enable standard log flags

	tiles := NewTileList()
	tiles.SetBorder(true).SetTitle(" [green]GoNewsDesk[white] · [yellow][F1[][white] [cadetblue]logs[white] · [yellow][q[][white] [cadetblue]quit[white] ")

	pages := tview.NewPages()
	pages.AddPage("desk", tiles, true, true)

	const newsModalPageID = "modal-news"
	var newsModalPreviousFocus tview.Primitive
	showNewsModal := func(item news.ExternalNews, body string) {
		if pages.HasPage(newsModalPageID) {
			pages.RemovePage(newsModalPageID)
		}
		newsModalPreviousFocus = app.GetFocus()

		textView := tview.NewTextView().
			SetScrollable(true).
			SetWrap(true).
			SetDynamicColors(true).
			SetText(newsDetailsText(item, body))
		textView.SetBorder(true).SetTitle(" News Details · [yellow][Esc[] [cadetblue]or [yellow][Enter[] [cadetblue]close[white] ")

		modal := tview.NewFlex().
			AddItem(nil, 0, 1, false).
			AddItem(tview.NewFlex().SetDirection(tview.FlexRow).
				AddItem(nil, 0, 1, false).
				AddItem(textView, 0, 4, true).
				AddItem(nil, 0, 1, false), 0, 4, true).
			AddItem(nil, 0, 1, false)

		modal.SetInputCapture(func(ev *tcell.EventKey) *tcell.EventKey {
			if ev.Key() == tcell.KeyEscape || ev.Key() == tcell.KeyEnter {
				pages.RemovePage(newsModalPageID)
				if newsModalPreviousFocus != nil {
					app.SetFocus(newsModalPreviousFocus)
				} else {
					app.SetFocus(tiles)
				}
				return nil
			}
			return ev
		})

		pages.AddPage(newsModalPageID, modal, true, true)
		app.SetFocus(textView)
	}

	if d.debug {
		log.Println("Debug mode enabled")
	}
	d.RunNewsStream(func(item news.ExternalNews) {
		tile := NewsTileFromExternal(item)
		tile.Highlight = headlineShouldHighlight(item.Headline, d.highlightKeywords)
		body := tile.Body
		tile.OnOpen = func() {
			showNewsModal(item, body)
		}
		tiles.AddTile(tile)
	})

	logBox.DataTable.SetApp(app)
	logBox.DataTable.SetReturnRoot(pages)

	const logModalPageID = "modal-logbox"
	var modalPreviousFocus tview.Primitive
	showLogModal := func() {
		if pages.HasPage(logModalPageID) {
			return
		}
		modalPreviousFocus = app.GetFocus()
		logBox.FlexBox.SetTitle(" Logs [yellow](Esc/F1 close)[white] ")
		modal := tview.NewFlex().
			AddItem(nil, 0, 1, false).
			AddItem(tview.NewFlex().SetDirection(tview.FlexRow).
				AddItem(nil, 0, 1, false).
				AddItem(logBox.FlexBox, 0, 3, true).
				AddItem(nil, 0, 1, false), 0, 3, true).
			AddItem(nil, 0, 1, false)

		modal.SetInputCapture(func(ev *tcell.EventKey) *tcell.EventKey {
			switch ev.Key() {
			case tcell.KeyEscape, tcell.KeyF1:
				pages.RemovePage(logModalPageID)
				if modalPreviousFocus != nil {
					app.SetFocus(modalPreviousFocus)
				} else {
					app.SetFocus(tiles)
				}
				return nil
			}
			return ev
		})

		pages.AddPage(logModalPageID, modal, true, true)
		app.SetFocus(logBox.TableView)
	}

	hideLogModal := func() bool {
		if !pages.HasPage(logModalPageID) {
			return false
		}
		pages.RemovePage(logModalPageID)
		if modalPreviousFocus != nil {
			app.SetFocus(modalPreviousFocus)
		} else {
			app.SetFocus(tiles)
		}
		return true
	}

	app.SetInputCapture(func(ev *tcell.EventKey) *tcell.EventKey {
		switch ev.Key() {
		case tcell.KeyF1:
			if !hideLogModal() {
				showLogModal()
			}
			return nil
		case tcell.KeyEscape:
			if hideLogModal() {
				return nil
			}
		}
		if ev.Key() == tcell.KeyRune && (ev.Rune() == 'q' || ev.Rune() == 'Q') {
			if d.onShutdown != nil {
				d.onShutdown()
			}
			return nil
		}
		return ev
	})

	return app.SetRoot(pages, true).SetFocus(tiles).Run()
}

// NewsTileFromExternal maps a merged news item to a scrollable tile.
func NewsTileFromExternal(n news.ExternalNews) Tile {
	body := strings.TrimSpace(sanitizeCountryFlags(n.Summary))
	if body == "" {
		body = strings.TrimSpace(sanitizeCountryFlags(n.Content))
	}
	meta := sanitizeCountryFlags(n.Source)
	if !n.Timestamp.IsZero() {
		if meta != "" {
			meta += " · "
		}
		meta += n.Timestamp.Local().Format(time.RFC3339)
	}
	if n.Author != "" {
		if meta != "" {
			meta += " · "
		}
		meta += sanitizeCountryFlags(n.Author)
	}
	if len(n.SymbolsMentioned) > 0 {
		if meta != "" {
			meta += " · "
		}
		meta += sanitizeCountryFlags(strings.Join(n.SymbolsMentioned, ", "))
	}
	var onOpen func()
	return Tile{
		Title:  sanitizeCountryFlags(n.Headline),
		Body:   body,
		Meta:   meta,
		OnOpen: onOpen,
	}
}

// RunNewsStream reads the processor merged channel and invokes handler on the
// tview UI thread for each item. It returns immediately; the goroutine stops
// when ctx is done or the stream channel closes. Requires a running app (e.g. after Run sets d.app).
func (d *NewsDesk) RunNewsStream(handler func(news.ExternalNews)) {
	if d == nil || d.NewsProcessor == nil || d.app == nil || handler == nil {
		return
	}
	ch := d.NewsProcessor.Stream()
	if ch == nil {
		return
	}
	go func() {
		for {
			select {
			case <-d.ctx.Done():
				return
			case msg, ok := <-ch:
				if !ok {
					return
				}
				m := msg
				d.app.QueueUpdateDraw(func() {
					handler(m)
				})
			}
		}
	}()
}

// RunNewsStreamTiles appends each incoming item to list as a tile.
func (d *NewsDesk) RunNewsStreamTiles(list *TileList) {
	if list == nil {
		return
	}
	d.RunNewsStream(func(item news.ExternalNews) {
		list.AddTile(NewsTileFromExternal(item))
	})
}

// App returns the tview application while Run is active.
func (d *NewsDesk) App() *tview.Application {
	if d == nil {
		return nil
	}
	return d.app
}

func headlineShouldHighlight(headline string, highlight []string) bool {
	h := strings.ToLower(headline)
	for _, kw := range highlight {
		kw = strings.ToLower(strings.TrimSpace(kw))
		if kw == "" {
			continue
		}
		if strings.Contains(h, kw) {
			return true
		}
	}
	return false
}

func newsDetailsText(item news.ExternalNews, body string) string {
	timestamp := ""
	if !item.Timestamp.IsZero() {
		timestamp = item.Timestamp.Local().Format(time.RFC3339)
	}
	symbols := strings.Join(item.SymbolsMentioned, ", ")

	var b strings.Builder
	fmt.Fprintf(&b, "[green]ID[-]: %s\n", tview.Escape(item.ID))
	fmt.Fprintf(&b, "[green]Headline[-]: %s\n", tview.Escape(sanitizeCountryFlags(item.Headline)))
	fmt.Fprintf(&b, "[green]Source[-]: %s\n", tview.Escape(sanitizeCountryFlags(item.Source)))
	fmt.Fprintf(&b, "[green]Author[-]: %s\n", tview.Escape(sanitizeCountryFlags(item.Author)))
	fmt.Fprintf(&b, "[green]Timestamp[-]: %s\n", tview.Escape(timestamp))
	fmt.Fprintf(&b, "[green]URL[-]: %s\n", tview.Escape(item.Url))
	fmt.Fprintf(&b, "[green]Symbols[-]: %s\n\n", tview.Escape(sanitizeCountryFlags(symbols)))
	fmt.Fprintf(&b, "[green]Body[-]:\n%s\n\n", tview.Escape(sanitizeCountryFlags(body)))
	fmt.Fprintf(&b, "[green]Summary[-]:\n%s\n\n", tview.Escape(sanitizeCountryFlags(item.Summary)))
	fmt.Fprintf(&b, "[green]Content[-]:\n%s\n", tview.Escape(sanitizeCountryFlags(item.Content)))
	return b.String()
}

func sanitizeCountryFlags(text string) string {
	if text == "" {
		return text
	}

	runes := []rune(text)
	var b strings.Builder
	b.Grow(len(text))

	for i := 0; i < len(runes); i++ {
		if i+1 < len(runes) && isRegionalIndicator(runes[i]) && isRegionalIndicator(runes[i+1]) {
			c1 := rune('A') + (runes[i] - regionalIndicatorA)
			c2 := rune('A') + (runes[i+1] - regionalIndicatorA)
			b.WriteString("[🏳 ")
			b.WriteRune(c1)
			b.WriteRune(c2)
			b.WriteRune(']')
			i++
			continue
		}
		b.WriteRune(runes[i])
	}

	return b.String()
}

func isRegionalIndicator(r rune) bool {
	return r >= regionalIndicatorA && r <= regionalIndicatorZ
}
