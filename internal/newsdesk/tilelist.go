package newsdesk

import (
	"github.com/gdamore/tcell/v2"
	"github.com/rivo/tview"
)

// ---- Model for one tile.
type Tile struct {
	Title     string // may contain [color] tags
	Body      string // may contain [color] tags
	Meta      string // subtitle line
	Highlight bool   // draws a yellow border around the tile
	OnOpen    func()
	// cache last measured height for current width to avoid recomputing too much (optional)
	lastWidth  int
	lastHeight int
}

func (t *Tile) MeasureHeight(innerWidth int) int {
	if innerWidth <= 0 {
		return 0
	}
	if t.lastWidth == innerWidth && t.lastHeight > 0 {
		return t.lastHeight
	}
	// We draw the tile with a 1-cell padding on left+right and top+bottom.
	contentWidth := innerWidth - 2
	if contentWidth < 1 {
		contentWidth = 1
	}
	titleLines := tview.WordWrap(t.Title, contentWidth)
	bodyLines := tview.WordWrap(t.Body, contentWidth)
	metaLines := 1 // shown as a single caption line

	// 2 for top/bottom padding, plus the lines themselves.
	h := 2 + len(titleLines) + len(bodyLines) + metaLines
	if h < 3 {
		h = 3
	}
	t.lastWidth, t.lastHeight = innerWidth, h
	return h
}

// ---- A scrollable/selectable vertical list with variable-height tiles.
type TileList struct {
	*tview.Box
	tiles         []Tile
	selectedIndex int
	scrollY       int // number of terminal rows scrolled from the very top
	selectedStyle tcell.Style
	normalStyle   tcell.Style
	lastInnerW    int
	lastInnerH    int
	skipKeepOnce  bool
}

func NewTileList() *TileList {
	return &TileList{
		Box:           tview.NewBox(),
		selectedStyle: tcell.StyleDefault.Background(tcell.NewHexColor(0x444c56)),
		normalStyle:   tcell.StyleDefault,
	}
}
func (tl *TileList) AddTile(t Tile) {
	wasAtBottom := tl.isAtBottom(tl.lastInnerH, tl.lastInnerW)
	stickSelectionToBottom := len(tl.tiles) > 0 && tl.selectedIndex == len(tl.tiles)-1
	tl.tiles = append(tl.tiles, t)
	if tl.selectedIndex < 0 {
		tl.selectedIndex = 0
	}
	if stickSelectionToBottom {
		tl.selectedIndex = len(tl.tiles) - 1
	}
	if wasAtBottom && tl.lastInnerW > 0 && tl.lastInnerH > 0 {
		total := tl.totalHeight(tl.lastInnerW)
		tl.scrollY = total - tl.lastInnerH
		if tl.scrollY < 0 {
			tl.scrollY = 0
		}
		// Keep bottom-follow behavior from being undone by selection visibility
		// enforcement during the next draw.
		tl.skipKeepOnce = true
	}
}

// helper: sum of heights up to (but not including) i at given width.
func (tl *TileList) yOfIndex(i, width int) int {
	y := 0
	for k := 0; k < i && k < len(tl.tiles); k++ {
		y += tl.tiles[k].MeasureHeight(width)
	}
	return y
}

func (tl *TileList) totalHeight(width int) int {
	if width <= 0 {
		return 0
	}
	total := 0
	for i := range tl.tiles {
		total += tl.tiles[i].MeasureHeight(width)
	}
	return total
}

func (tl *TileList) isAtBottom(viewHeight, innerWidth int) bool {
	if viewHeight <= 0 || innerWidth <= 0 {
		return false
	}
	total := tl.totalHeight(innerWidth)
	return tl.scrollY+viewHeight >= total
}

// ensure the selected tile is fully visible inside the viewport [scrollY, scrollY+h)
func (tl *TileList) keepSelectionVisible(viewHeight, innerWidth int) {
	if len(tl.tiles) == 0 {
		// normalize selection when no tiles
		tl.selectedIndex = 0
		tl.scrollY = 0
		return
	}
	// Clamp selection into valid range
	if tl.selectedIndex < 0 {
		tl.selectedIndex = 0
	}
	if tl.selectedIndex >= len(tl.tiles) {
		tl.selectedIndex = len(tl.tiles) - 1
	}
	top := tl.yOfIndex(tl.selectedIndex, innerWidth)
	selH := tl.tiles[tl.selectedIndex].MeasureHeight(innerWidth)
	bot := top + selH

	// if above: scroll up
	if top < tl.scrollY {
		tl.scrollY = top
	}
	// if below: scroll down so bottom aligns
	if bot > tl.scrollY+viewHeight {
		tl.scrollY = bot - viewHeight
	}
	if tl.scrollY < 0 {
		tl.scrollY = 0
	}
}

func (tl *TileList) Draw(screen tcell.Screen) {
	tl.Box.DrawForSubclass(screen, tl)
	x, y, w, h := tl.GetInnerRect()
	tl.lastInnerW, tl.lastInnerH = w, h
	if w <= 0 || h <= 0 {
		return
	}
	// Avoid awkward rendering on extremely narrow widths
	if w < 3 {
		return
	}

	// Ensure non-negative selected index even before measuring
	if tl.selectedIndex < 0 {
		tl.selectedIndex = 0
	}

	// Keep selection in view given the current width/height.
	// When auto-following the bottom on new items, skip once so the new tile
	// remains visible while selection stays unchanged.
	if tl.skipKeepOnce {
		tl.skipKeepOnce = false
	} else {
		tl.keepSelectionVisible(h, w)
	}

	// Additional safety: if tiles got removed asynchronously between measure and draw
	if tl.selectedIndex < 0 {
		tl.selectedIndex = 0
	}
	if tl.selectedIndex >= len(tl.tiles) && len(tl.tiles) > 0 {
		tl.selectedIndex = len(tl.tiles) - 1
	}

	// Find first visible tile by consuming heights until we pass scrollY.
	curY := 0
	i := 0
	for i < len(tl.tiles) {
		th := tl.tiles[i].MeasureHeight(w)
		if curY+th > tl.scrollY {
			break
		}
		curY += th
		i++
	}
	drawY := y - (tl.scrollY - curY)

	// Draw visible tiles
	for ; i < len(tl.tiles) && drawY < y+h; i++ {
		t := &tl.tiles[i]
		th := t.MeasureHeight(w)
		style := tl.normalStyle
		if i == tl.selectedIndex {
			style = tl.selectedStyle
		}
		// background highlight block (clamped to viewport)
		maxY := drawY + th
		if maxY > y+h {
			maxY = y + h
		}
		startY := drawY
		if startY < y {
			startY = y
		}
		for yy := startY; yy < maxY; yy++ {
			startX := x
			if startX < 0 {
				startX = 0
			}
			for xx := startX; xx < x+w; xx++ {
				screen.SetContent(xx, yy, ' ', nil, style)
			}
		}
		if t.Highlight && w >= 2 && th >= 2 {
			_, bg, _ := style.Decompose()
			borderStyle := style.Foreground(tcell.ColorYellow).Background(bg)
			top := drawY
			bottom := drawY + th - 1
			left := x
			right := x + w - 1

			if top >= y && top < y+h {
				for xx := left; xx <= right; xx++ {
					screen.SetContent(xx, top, tcell.RuneHLine, nil, borderStyle)
				}
			}
			if bottom >= y && bottom < y+h {
				for xx := left; xx <= right; xx++ {
					screen.SetContent(xx, bottom, tcell.RuneHLine, nil, borderStyle)
				}
			}

			startBorderY := top
			if startBorderY < y {
				startBorderY = y
			}
			endBorderY := bottom
			if endBorderY >= y+h {
				endBorderY = y + h - 1
			}
			for yy := startBorderY; yy <= endBorderY; yy++ {
				screen.SetContent(left, yy, tcell.RuneVLine, nil, borderStyle)
				screen.SetContent(right, yy, tcell.RuneVLine, nil, borderStyle)
			}
			if top >= y && top < y+h {
				screen.SetContent(left, top, tcell.RuneULCorner, nil, borderStyle)
				screen.SetContent(right, top, tcell.RuneURCorner, nil, borderStyle)
			}
			if bottom >= y && bottom < y+h {
				screen.SetContent(left, bottom, tcell.RuneLLCorner, nil, borderStyle)
				screen.SetContent(right, bottom, tcell.RuneLRCorner, nil, borderStyle)
			}
		}

		// Tile content (simple hand-drawn; swap with real child widgets if you prefer):
		tx := x + 1
		tw := w - 2
		if tw < 1 {
			tw = 1
		}
		line := drawY + 1

		// Meta (one line at top)
		if line >= y && line < y+h {
			tview.Print(screen, t.Meta, tx, line, tw, tview.AlignLeft, tcell.ColorGray)
		}
		line++

		// Title
		for _, l := range tview.WordWrap(t.Title, tw) {
			if line >= y && line < y+h {
				tview.Print(screen, l, tx, line, tw, tview.AlignLeft, tcell.ColorCadetBlue)
			}
			line++
		}
		// Body
		for _, l := range tview.WordWrap(t.Body, tw) {
			if line >= y && line < y+h {
				tview.Print(screen, l, tx, line, tw, tview.AlignLeft, tcell.ColorLightBlue)
			}
			line++
		}
		// advance to next tile
		drawY += th
	}
}

func (tl *TileList) InputHandler() func(*tcell.EventKey, func(tview.Primitive)) {
	return func(ev *tcell.EventKey, _ func(tview.Primitive)) {
		switch ev.Key() {
		case tcell.KeyUp:
			if tl.selectedIndex > 0 {
				tl.selectedIndex--
			} else if tl.selectedIndex < 0 {
				tl.selectedIndex = 0
			}
		case tcell.KeyDown:
			if len(tl.tiles) > 0 {
				if tl.selectedIndex < len(tl.tiles)-1 {
					tl.selectedIndex++
				} else if tl.selectedIndex >= len(tl.tiles) {
					tl.selectedIndex = len(tl.tiles) - 1
				}
			} else {
				tl.selectedIndex = 0
			}
		case tcell.KeyPgUp:
			// scroll up ~h rows
			_, _, _, h := tl.GetInnerRect()
			tl.scrollY -= h
			if tl.scrollY < 0 {
				tl.scrollY = 0
			}
		case tcell.KeyPgDn:
			_, _, _, h := tl.GetInnerRect()
			tl.scrollY += h
		case tcell.KeyHome:
			tl.selectedIndex, tl.scrollY = 0, 0
		case tcell.KeyEnd:
			if len(tl.tiles) > 0 {
				tl.selectedIndex = len(tl.tiles) - 1
			} else {
				tl.selectedIndex = 0
			}
		case tcell.KeyEnter:
			if tl.selectedIndex >= 0 && tl.selectedIndex < len(tl.tiles) {
				if f := tl.tiles[tl.selectedIndex].OnOpen; f != nil {
					f()
				}
			}
		default:
			if parentHandler := tl.Box.InputHandler(); parentHandler != nil {
				parentHandler(ev, nil)
			}
		}
	}
}

func (tl *TileList) Focus(delegate func(tview.Primitive)) {
	delegate(tl.Box)
}

func (tl *TileList) HasFocus() bool {
	return tl.Box.HasFocus()
}
