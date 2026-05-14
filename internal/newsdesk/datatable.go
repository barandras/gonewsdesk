package newsdesk

import (
	"fmt"
	"log"
	"strings"

	"github.com/gdamore/tcell/v2"
	"github.com/rivo/tview"
)

const (
	defaultHeaderBackgroundColor = tcell.ColorDarkBlue
	defaultHeaderTextColor       = tcell.ColorWhite
	defaultRowBackgroundColor    = tcell.ColorDefault
	defaultRowTextColor          = tcell.ColorLightBlue
)

type ColoringApplyTo string

const (
	ColoringApplyToRow  ColoringApplyTo = "row"
	ColoringApplyToCell ColoringApplyTo = "cell"
)

type ColoringCondition struct {
	Keyword         string
	TextColor       *tcell.Color
	BackgroundColor *tcell.Color
	ApplyTo         ColoringApplyTo
}

// DataTable is the main table object that contains the header and rows
type DataTable struct {
	tview.TableContentReadOnly
	header               []string
	displayHeader        []string     // Headers to actually display
	hiddenColumns        map[int]bool // Track which columns are hidden
	rows                 *[][]string
	maxRows              *int
	table                *tview.Table // Reference to the actual table widget
	app                  *tview.Application
	returnRoot           tview.Primitive
	customDetailsBuilder func(row []string) *tview.Flex // Custom modal builder function
	coloringConditions   []ColoringCondition
}

// NewDataTableParams is the parameters for creating a new DataTable
type NewDataTableParams struct {
	header        []string
	hiddenColumns []int // List of column indices to hide
	rows          *[][]string
	maxRows       *int
}

// NewDataTable creates a new DataTable
func NewDataTable(params NewDataTableParams) *DataTable {
	hiddenMap := make(map[int]bool)
	for _, col := range params.hiddenColumns {
		hiddenMap[col] = true
	}

	// Build display header (without hidden columns)
	displayHeader := make([]string, 0)
	for i, h := range params.header {
		if !hiddenMap[i] {
			displayHeader = append(displayHeader, h)
		}
	}

	return &DataTable{
		header:        params.header,
		displayHeader: displayHeader,
		hiddenColumns: hiddenMap,
		rows:          params.rows,
		maxRows:       params.maxRows,
	}
}

func (t *DataTable) GetCell(row, column int) *tview.TableCell {
	// Convert display column to actual column by accounting for hidden columns
	actualColumn := t.displayColumnToActualColumn(column)
	if actualColumn == -1 {
		return nil
	}

	// if it's row 0, return the header cell
	if row == 0 {
		cell := tview.NewTableCell(t.displayHeader[column]).
			SetBackgroundColor(defaultHeaderBackgroundColor).
			SetTextColor(defaultHeaderTextColor).
			SetSelectable(false)

		// Make the last column expand
		if column == len(t.displayHeader)-1 {
			cell.SetExpansion(1)
		}
		return cell
	}

	// if the row greater than 0, return the row cell
	if row-1 < len(*t.rows) {
		textColor := defaultRowTextColor
		backgroundColor := defaultRowBackgroundColor

		if len(t.coloringConditions) > 0 {
			for _, condition := range t.coloringConditions {
				for cellIndex, cellInRow := range (*t.rows)[row-1] {
					if condition.Keyword == cellInRow {
						// Check if the condition should apply to the current cell
						if condition.ApplyTo == ColoringApplyToRow {
							// Apply to all cells in the row
							if condition.TextColor != nil {
								textColor = *condition.TextColor
							}
							if condition.BackgroundColor != nil {
								backgroundColor = *condition.BackgroundColor
							}
						} else if condition.ApplyTo == ColoringApplyToCell && cellIndex == actualColumn {
							// Only apply if the matching cell is the current cell
							if condition.TextColor != nil {
								textColor = *condition.TextColor
							}
							if condition.BackgroundColor != nil {
								backgroundColor = *condition.BackgroundColor
							}
						}
					}
				}
			}
		}

		cell := tview.NewTableCell((*t.rows)[row-1][actualColumn]).
			SetBackgroundColor(backgroundColor).
			SetTextColor(textColor).
			SetSelectable(true)

		// Make the last column expand
		if column == len(t.displayHeader)-1 {
			cell.SetExpansion(1)
		}
		return cell
	}

	// if the row is greater than the number of rows, return an empty cell
	log.Println("DataTable: getCell error - requested row is greater than the number of rows", row, len(*t.rows))
	return nil
}

func (t *DataTable) GetRowCount() int {
	return len(*t.rows) + 1 // rows + header
}

func (t *DataTable) GetColumnCount() int {
	return len(t.displayHeader) // Return display column count, not actual
}

// displayColumnToActualColumn converts a display column index to the actual column index
func (t *DataTable) displayColumnToActualColumn(displayCol int) int {
	actualCol := 0
	displayCount := 0

	for actualCol < len(t.header) {
		if !t.hiddenColumns[actualCol] {
			if displayCount == displayCol {
				return actualCol
			}
			displayCount++
		}
		actualCol++
	}
	return -1 // Should not happen if displayCol is valid
}

// SetTable sets the reference to the tview.Table widget
func (t *DataTable) SetTable(table *tview.Table) {
	t.table = table
	// Wire Enter to open the row details modal (header row is row 0)
	if t.table != nil {
		t.table.SetSelectedFunc(func(row, column int) {
			if row <= 0 {
				return
			}
			t.openDetailsModal(row - 1) // convert to data row index
		})
	}
}

// SetApp sets the reference to the tview.Application
func (t *DataTable) SetApp(app *tview.Application) {
	t.app = app
}

// GetSelectedRow returns the currently selected row
func (t *DataTable) GetSelectedRow() int {
	if t.table != nil {
		row, _ := t.table.GetSelection()
		return row
	}
	return -1
}

// ScrollToLastRow scrolls to the last row
func (t *DataTable) ScrollToLastRow() {
	if t.table != nil {
		t.table.ScrollToEnd()
	}
}

// SetSelectedRow sets the selected row
func (t *DataTable) SetSelectedRow(row int) {
	if t.table != nil {
		t.table.Select(row, 0)
	}
}

// AddRow adds a new row to the table with auto-scroll logic
func (t *DataTable) AddRow(newRow []string, redraw bool) {
	// Check if we should follow before adding the new row
	shouldFollow := t.GetSelectedRow() == t.GetRowCount()-1

	// Add the new row
	*t.rows = append(*t.rows, newRow)

	// Check if we should limit the number of rows
	if t.maxRows != nil && len(*t.rows) >= *t.maxRows+1 {
		*t.rows = (*t.rows)[1:]
	}

	// Auto-scroll if the last row was selected
	if shouldFollow {
		t.ScrollToLastRow()
		t.SetSelectedRow(t.GetRowCount() - 1)
	}

	// Trigger table refresh
	if redraw && t.app != nil {
		t.app.QueueUpdateDraw(func() {})
	}
}

// FlushRows flushes the rows to the table
func (t *DataTable) FlushRows(redraw bool) {
	// empty out the rows matrix and trigger a refresh
	*t.rows = [][]string{}
	if redraw {
		if t.app != nil {
			t.app.QueueUpdateDraw(func() {})
		}
	}
}

// FindRowByValue finds a row by matching a value in a specific column
// Returns the row index (0-based) or -1 if not found
func (t *DataTable) FindRowByValue(column int, value string) int {
	if column < 0 || column >= len(t.header) {
		return -1
	}

	for i, row := range *t.rows {
		if len(row) > column && row[column] == value {
			return i
		}
	}
	return -1
}

// UpdateRow updates an existing row at the specified index
func (t *DataTable) UpdateRow(rowIndex int, newRow []string, redraw bool) {
	if rowIndex < 0 || rowIndex >= len(*t.rows) {
		return
	}

	// Update the row
	(*t.rows)[rowIndex] = newRow

	// Trigger table refresh
	if redraw && t.app != nil {
		t.app.QueueUpdateDraw(func() {})
	}
}

// AddOrUpdateRowByValue adds a new row or updates an existing one based on a unique value in a specific column
func (t *DataTable) AddOrUpdateRowByValue(uniqueColumn int, newRow []string, redraw bool) {
	if uniqueColumn < 0 || uniqueColumn >= len(newRow) {
		// Invalid column, just add as new row
		t.AddRow(newRow, redraw)
		return
	}

	uniqueValue := newRow[uniqueColumn]
	existingRowIndex := t.FindRowByValue(uniqueColumn, uniqueValue)

	if existingRowIndex >= 0 {
		// Update existing row
		t.UpdateRow(existingRowIndex, newRow, redraw)
	} else {
		// Add new row
		t.AddRow(newRow, redraw)
	}
}

// SetCustomModalBuilder sets a custom function to build the details modal.
// If set, this function will be called instead of the built-in modal.
// The function receives the full row data and should return a *tview.Flex.
func (t *DataTable) SetCustomModalBuilder(builder func(row []string) *tview.Flex) {
	t.customDetailsBuilder = builder
}

// SetReturnRoot sets the root (usually *tview.Pages) used to overlay the modal.
func (t *DataTable) SetReturnRoot(root tview.Primitive) {
	t.returnRoot = root
}

// openDetailsModal builds and shows a modal with visible column values of the selected row.
func (t *DataTable) openDetailsModal(dataRow int) {
	if t.app == nil || t.returnRoot == nil || t.rows == nil || t.table == nil {
		return
	}
	if dataRow < 0 || dataRow >= len(*t.rows) {
		return
	}

	// Use custom modal builder if provided
	var modal *tview.Flex
	if t.customDetailsBuilder != nil {
		modal = t.customDetailsBuilder((*t.rows)[dataRow])
	} else {
		// Build detail text with aligned value column.
		var b strings.Builder
		row := (*t.rows)[dataRow]

		// 1) Find max visible header width.
		maxLabel := 0
		for actualCol := 0; actualCol < len(t.header); actualCol++ {
			if t.hiddenColumns[actualCol] {
				continue
			}
			if l := len(t.header[actualCol]); l > maxLabel {
				maxLabel = l
			}
		}

		// 2) Print each field with padded label so values align.
		for actualCol := 0; actualCol < len(t.header); actualCol++ {
			if t.hiddenColumns[actualCol] {
				continue
			}
			h := t.header[actualCol]
			val := ""
			if actualCol < len(row) {
				val = row[actualCol]
			}
			fmt.Fprintf(&b, "[green]%-*s[-]: %s\n", maxLabel, h, val)
		}

		textView := tview.NewTextView().
			SetScrollable(true).
			SetWrap(true).
			SetDynamicColors(true).
			SetText(b.String())
		textView.SetBorder(true).SetTitle("Details")

		// Centered modal layout
		modal = tview.NewFlex().
			AddItem(nil, 0, 1, false).
			AddItem(tview.NewFlex().SetDirection(tview.FlexRow).
				AddItem(nil, 0, 1, false).
				AddItem(textView, 0, 3, true).
				AddItem(nil, 0, 1, false), 0, 3, true).
			AddItem(nil, 0, 1, false)

	}

	// Save/restore focus
	currentFocus := t.app.GetFocus()
	pageID := fmt.Sprintf("modal-dt-%p", t.table)

	modal.SetInputCapture(func(event *tcell.EventKey) *tcell.EventKey {
		switch event.Key() {
		case tcell.KeyEscape, tcell.KeyEnter:
			if pages, ok := t.returnRoot.(*tview.Pages); ok {
				pages.RemovePage(pageID)
				t.app.SetFocus(currentFocus)
			}
			return nil
		}
		return event
	})

	if pages, ok := t.returnRoot.(*tview.Pages); ok {
		pages.AddPage(pageID, modal, true, true)
		t.app.SetFocus(modal)
	}
}

// AddColoringConditions adds a new (or set of) coloring condition to the table
func (t *DataTable) AddColoringConditions(conditions ...ColoringCondition) {
	t.coloringConditions = append(t.coloringConditions, conditions...)
}

func colorPtr(color tcell.Color) *tcell.Color {
	return &color
}
