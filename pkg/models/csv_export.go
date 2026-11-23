// Vikunja is a to-do list application to facilitate your life.
// Copyright 2018-present Vikunja and contributors. All rights reserved.
//
// This program is free software: you can redistribute it and/or modify
// it under the terms of the GNU Affero General Public License as published by
// the Free Software Foundation, either version 3 of the License, or
// (at your option) any later version.
//
// This program is distributed in the hope that it will be useful,
// but WITHOUT ANY WARRANTY; without even the implied warranty of
// MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE.  See the
// GNU Affero General Public License for more details.
//
// You should have received a copy of the GNU Affero General Public License
// along with this program.  If not, see <https://www.gnu.org/licenses/>.

package models

import (
	"bytes"
	"encoding/csv"
	"strings"
	"unicode"
)

// csvBOM makes Excel read the file as UTF-8, so Turkish characters survive.
const csvBOM = "\xef\xbb\xbf"

// csvSafeCell neutralizes a cell that a spreadsheet would run as a formula: one starting with =, +, -, @, a tab or a
// carriage return gets a single quote in front, and so does one whose first character after leading whitespace is one
// of them (spreadsheets skip the whitespace). Numbers the exports write themselves are formatted by us and must not
// pass through here, a negative number would be damaged.
func csvSafeCell(v string) string {
	if v == "" {
		return v
	}
	if v[0] == '\t' || v[0] == '\r' {
		return "'" + v
	}
	if rest := strings.TrimLeftFunc(v, unicode.IsSpace); rest != "" {
		switch rest[0] {
		case '=', '+', '-', '@':
			return "'" + v
		}
	}
	return v
}

// CSVTable is a header and rows of cells. Cells in text are guarded against formula injection, cells in raw are not
// (they are numbers or dates formatted by the server).
type CSVTable struct {
	Header []string
	Rows   [][]CSVCell
}

// CSVCell is one cell of a CSVTable.
type CSVCell struct {
	Value string
	// Raw cells hold numbers and dates written by the server and are not guarded.
	Raw bool
}

func textCell(v string) CSVCell { return CSVCell{Value: v} }
func rawCell(v string) CSVCell  { return CSVCell{Value: v, Raw: true} }

// Bytes encodes the table as CSV: UTF-8 with a byte order mark, comma separated, CRLF line ends, fields quoted
// where needed.
func (t *CSVTable) Bytes() ([]byte, error) {
	var buf bytes.Buffer
	buf.WriteString(csvBOM)
	w := csv.NewWriter(&buf)
	w.UseCRLF = true
	header := make([]string, len(t.Header))
	for i, h := range t.Header {
		header[i] = csvSafeCell(h)
	}
	if err := w.Write(header); err != nil {
		return nil, err
	}
	for _, row := range t.Rows {
		out := make([]string, len(row))
		for i, c := range row {
			if c.Raw {
				out[i] = c.Value
			} else {
				out[i] = csvSafeCell(strings.ToValidUTF8(c.Value, "�"))
			}
		}
		if err := w.Write(out); err != nil {
			return nil, err
		}
	}
	w.Flush()
	return buf.Bytes(), w.Error()
}
