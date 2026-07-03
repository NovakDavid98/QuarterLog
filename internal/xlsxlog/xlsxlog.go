// Package xlsxlog appends worklog entries to a nicely styled local .xlsx file.
package xlsxlog

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/xuri/excelize/v2"
)

const sheet = "Worklog"

// Columns of the worklog table, in order.
var headers = []string{"Day", "Hours", "Category from order", "Description", "Type", "Month"}

// Entry is a single worklog row. Month is derived from Day at write time.
type Entry struct {
	Day         time.Time
	Hours       float64
	Category    string
	Description string
	Type        string
}

var mu sync.Mutex

// ErrFileOpen indicates the workbook is locked, almost always because it is open
// in Excel.
var ErrFileOpen = errors.New("the worklog file is open in Excel — close it and try again")

// Append writes one entry as a new row, creating and styling the file if needed.
func Append(path string, e Entry) error {
	mu.Lock()
	defer mu.Unlock()

	if path == "" {
		return fmt.Errorf("worklog file path is not set")
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}

	f, err := open(path)
	if err != nil {
		return err
	}
	defer f.Close()

	styles, err := ensureStyles(f)
	if err != nil {
		return err
	}

	rows, err := f.GetRows(sheet)
	if err != nil {
		return err
	}
	row := len(rows) + 1 // 1-based; header is row 1

	set := func(col string, v interface{}, style int) error {
		cell := fmt.Sprintf("%s%d", col, row)
		if err := f.SetCellValue(sheet, cell, v); err != nil {
			return err
		}
		return f.SetCellStyle(sheet, cell, cell, style)
	}

	if err := set("A", e.Day, styles.date); err != nil {
		return err
	}
	if err := set("B", e.Hours, styles.hours); err != nil {
		return err
	}
	if err := set("C", e.Category, styles.body); err != nil {
		return err
	}
	if err := set("D", e.Description, styles.wrap); err != nil {
		return err
	}
	if err := set("E", e.Type, styles.body); err != nil {
		return err
	}
	if err := set("F", e.Day.Format("2006-01"), styles.body); err != nil {
		return err
	}

	// Keep the autofilter covering all data rows.
	_ = f.AutoFilter(sheet, fmt.Sprintf("A1:F%d", row), nil)

	if err := f.SaveAs(path); err != nil {
		if isLocked(err) {
			return ErrFileOpen
		}
		return err
	}
	return nil
}

// Clear empties the worklog: it overwrites the file with a fresh, styled,
// header-only workbook (removing every logged row).
func Clear(path string) error {
	mu.Lock()
	defer mu.Unlock()
	if path == "" {
		return fmt.Errorf("worklog file path is not set")
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	f, err := newFile()
	if err != nil {
		return err
	}
	defer f.Close()
	if err := f.SaveAs(path); err != nil {
		if isLocked(err) {
			return ErrFileOpen
		}
		return err
	}
	return nil
}

// EnsureFile creates an empty, styled worklog file at path if it doesn't exist.
func EnsureFile(path string) error {
	mu.Lock()
	defer mu.Unlock()
	if _, err := os.Stat(path); err == nil {
		return nil
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	f, err := newFile()
	if err != nil {
		return err
	}
	defer f.Close()
	if err := f.SaveAs(path); err != nil {
		if isLocked(err) {
			return ErrFileOpen
		}
		return err
	}
	return nil
}

// open loads an existing workbook, or builds a freshly styled one.
func open(path string) (*excelize.File, error) {
	if _, err := os.Stat(path); err == nil {
		f, err := excelize.OpenFile(path)
		if err != nil {
			if isLocked(err) {
				return nil, ErrFileOpen
			}
			return nil, err
		}
		return f, nil
	}
	return newFile()
}

func newFile() (*excelize.File, error) {
	f := excelize.NewFile()
	// Rename the default sheet to Worklog.
	if err := f.SetSheetName(f.GetSheetName(0), sheet); err != nil {
		return nil, err
	}

	styles, err := ensureStyles(f)
	if err != nil {
		return nil, err
	}

	for i, h := range headers {
		col, _ := excelize.ColumnNumberToName(i + 1)
		cell := col + "1"
		if err := f.SetCellValue(sheet, cell, h); err != nil {
			return nil, err
		}
		if err := f.SetCellStyle(sheet, cell, cell, styles.header); err != nil {
			return nil, err
		}
	}

	widths := map[string]float64{"A": 12, "B": 8, "C": 22, "D": 62, "E": 12, "F": 10}
	for col, w := range widths {
		_ = f.SetColWidth(sheet, col, col, w)
	}
	_ = f.SetRowHeight(sheet, 1, 22)

	// Freeze the header row.
	_ = f.SetPanes(sheet, &excelize.Panes{
		Freeze:      true,
		YSplit:      1,
		TopLeftCell: "A2",
		ActivePane:  "bottomLeft",
	})
	_ = f.AutoFilter(sheet, "A1:F1", nil)

	return f, nil
}

type styleSet struct {
	header, body, date, hours, wrap int
}

// ensureStyles builds (once per open file) the style IDs the writer uses.
func ensureStyles(f *excelize.File) (styleSet, error) {
	var s styleSet
	var err error

	thin := []excelize.Border{
		{Type: "left", Color: "E2E2E2", Style: 1},
		{Type: "right", Color: "E2E2E2", Style: 1},
		{Type: "top", Color: "E2E2E2", Style: 1},
		{Type: "bottom", Color: "E2E2E2", Style: 1},
	}

	if s.header, err = f.NewStyle(&excelize.Style{
		Font:      &excelize.Font{Bold: true, Size: 11, Color: "1C1C1E"},
		Fill:      excelize.Fill{Type: "pattern", Pattern: 1, Color: []string{"F2F2F4"}},
		Alignment: &excelize.Alignment{Vertical: "center", Horizontal: "left"},
		Border: []excelize.Border{
			{Type: "bottom", Color: "C8C8CC", Style: 2},
		},
	}); err != nil {
		return s, err
	}

	if s.body, err = f.NewStyle(&excelize.Style{
		Font:      &excelize.Font{Size: 11},
		Alignment: &excelize.Alignment{Vertical: "center"},
		Border:    thin,
	}); err != nil {
		return s, err
	}

	if s.date, err = f.NewStyle(&excelize.Style{
		Font:         &excelize.Font{Size: 11},
		Alignment:    &excelize.Alignment{Vertical: "center"},
		Border:       thin,
		CustomNumFmt: strptr("m/d/yyyy"),
	}); err != nil {
		return s, err
	}

	if s.hours, err = f.NewStyle(&excelize.Style{
		Font:         &excelize.Font{Size: 11},
		Alignment:    &excelize.Alignment{Vertical: "center", Horizontal: "right"},
		Border:       thin,
		CustomNumFmt: strptr("0.00"),
	}); err != nil {
		return s, err
	}

	if s.wrap, err = f.NewStyle(&excelize.Style{
		Font:      &excelize.Font{Size: 11},
		Alignment: &excelize.Alignment{Vertical: "center", WrapText: true},
		Border:    thin,
	}); err != nil {
		return s, err
	}

	return s, nil
}

func strptr(s string) *string { return &s }

// isLocked reports whether err looks like a Windows sharing violation.
func isLocked(err error) bool {
	if err == nil {
		return false
	}
	msg := strings.ToLower(err.Error())
	return strings.Contains(msg, "being used by another process") ||
		strings.Contains(msg, "sharing violation") ||
		strings.Contains(msg, "access is denied") ||
		strings.Contains(msg, "permission denied")
}
