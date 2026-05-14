package newsdesk

import (
	"context"
	"fmt"
	"regexp"
	"strings"
	"sync"

	"github.com/gdamore/tcell/v2"
	"github.com/rivo/tview"
)

// logChannelWriter implements io.Writer to send log messages to a channel
type logChannelWriter struct {
	ch chan LogEntry
}

func (w *logChannelWriter) Write(p []byte) (n int, err error) {
	// Parse the log line and send it to the channel
	logLine := strings.TrimSuffix(string(p), "\n")
	entry := parseLogEntry(logLine)
	w.ch <- entry
	return len(p), nil
}

type LogBox struct {
	ctx       context.Context
	logCh     chan LogEntry
	once      sync.Once
	DataTable *DataTable
	TableView *tview.Table
	FlexBox   *tview.Flex
}

type LogEntry struct {
	Date    string
	Time    string
	File    string
	Line    string
	Message string
}

type NewLogBoxParams struct {
	Ctx context.Context
}

func newLogBox(ctx context.Context) *LogBox {
	logBox := &LogBox{
		ctx:   ctx,
		logCh: make(chan LogEntry, 100), // buffered channel to prevent blocking
		DataTable: NewDataTable(NewDataTableParams{
			header: []string{"Date", "Time", "File", "Line", "Message"},
			rows:   &[][]string{},
		}),
	}

	logBox.DataTable.AddColoringConditions(
		ColoringCondition{Keyword: "error", TextColor: colorPtr(tcell.ColorRed), ApplyTo: ColoringApplyToCell},
		ColoringCondition{Keyword: "warning", TextColor: colorPtr(tcell.ColorYellow), ApplyTo: ColoringApplyToCell},
		ColoringCondition{Keyword: "info", TextColor: colorPtr(tcell.ColorBlue), ApplyTo: ColoringApplyToCell},
		ColoringCondition{Keyword: "debug", TextColor: colorPtr(tcell.ColorGray), ApplyTo: ColoringApplyToCell},
	)
	logBox.DataTable.SetCustomModalBuilder(func(row []string) *tview.Flex {
		labels := []string{"Date", "Time", "File", "Line", "Message"}
		maxLabel := 0
		for _, label := range labels {
			if len(label) > maxLabel {
				maxLabel = len(label)
			}
		}

		var b strings.Builder
		for i, label := range labels {
			value := ""
			if i < len(row) {
				value = row[i]
			}
			fmt.Fprintf(&b, "[green]%-*s[-]: %s\n", maxLabel, label, value)
		}

		textView := tview.NewTextView().
			SetScrollable(true).
			SetWrap(true).
			SetDynamicColors(true).
			SetText(b.String())
		textView.SetBorder(true).SetTitle(" Details ")

		// Slightly smaller than the logbox modal.
		return tview.NewFlex().
			AddItem(nil, 0, 1, false).
			AddItem(tview.NewFlex().SetDirection(tview.FlexRow).
				AddItem(nil, 0, 1, false).
				AddItem(textView, 0, 2, true).
				AddItem(nil, 0, 1, false), 0, 2, true).
			AddItem(nil, 0, 1, false)
	})

	logBox.TableView = tview.NewTable().
		SetContent(logBox.DataTable).
		SetFixed(1, 0).
		SetSelectable(true, false).
		SetSelectedStyle(tcell.StyleDefault.Background(tcell.ColorDarkGray).Foreground(tcell.ColorWhite))

	// Connect the table reference to the DataTable (wires Enter on a row to open details modal).
	logBox.DataTable.SetTable(logBox.TableView)

	// Wrap log table in a box with border and title
	logBox.FlexBox = tview.NewFlex().
		SetDirection(tview.FlexRow).
		AddItem(logBox.TableView, 0, 1, true)

	logBox.FlexBox.SetBorder(true).SetTitle("Logs")

	return logBox
}

func (l *LogBox) GetLogChAndStart() chan LogEntry {
	l.once.Do(func() {
		go func() {
			for {
				select {
				case <-l.ctx.Done():
					return
				case entry := <-l.logCh:
					l.DataTable.AddRow([]string{entry.Date, entry.Time, entry.File, entry.Line, entry.Message}, true)
				}
			}
		}()
	})
	return l.logCh
}

// parseLogEntry parses a log line formatted with standard log flags
// Expected format: "2006/01/02 15:04:05 file.go:123: message"
func parseLogEntry(logLine string) LogEntry {
	// Regex to match: date time file:line: message
	// Pattern: YYYY/MM/DD HH:MM:SS file.go:line: message
	pattern := `^(\d{4}/\d{2}/\d{2})\s+(\d{2}:\d{2}:\d{2})\s+([^:]+):(\d+):\s*(.*)$`
	re := regexp.MustCompile(pattern)

	matches := re.FindStringSubmatch(strings.TrimSpace(logLine))
	if len(matches) == 6 {
		return LogEntry{
			Date:    matches[1],
			Time:    matches[2],
			File:    matches[3],
			Line:    matches[4],
			Message: matches[5],
		}
	}

	// Fallback: if parsing fails, put everything in message
	return LogEntry{
		Date:    "",
		Time:    "",
		File:    "",
		Line:    "",
		Message: logLine,
	}
}
