// Command libghostty-smoke verifies that the libghostty-vt binding links and
// works in our build environment. It is a throwaway diagnostic, not part of
// the public API.
package main

import (
	"fmt"
	"log"

	libghostty "github.com/mitchellh/go-libghostty"
)

func main() {
	term, err := libghostty.NewTerminal(libghostty.WithSize(20, 3))
	if err != nil {
		log.Fatalf("NewTerminal: %v", err)
	}
	defer term.Close()

	term.VTWrite([]byte("hello\r\n"))

	x, err := term.CursorX()
	if err != nil {
		log.Fatalf("CursorX: %v", err)
	}
	y, err := term.CursorY()
	if err != nil {
		log.Fatalf("CursorY: %v", err)
	}
	fmt.Printf("cursor=(%d,%d)\n", x, y)

	rs, err := libghostty.NewRenderState()
	if err != nil {
		log.Fatalf("NewRenderState: %v", err)
	}
	defer rs.Close()
	if err := rs.Update(term); err != nil {
		log.Fatalf("RenderState.Update: %v", err)
	}

	rows, _ := rs.Rows()
	cols, _ := rs.Cols()
	fmt.Printf("viewport=%dx%d\n", cols, rows)

	ri, err := libghostty.NewRenderStateRowIterator()
	if err != nil {
		log.Fatalf("NewRenderStateRowIterator: %v", err)
	}
	defer ri.Close()
	if err := rs.RowIterator(ri); err != nil {
		log.Fatalf("rs.RowIterator: %v", err)
	}

	rc, err := libghostty.NewRenderStateRowCells()
	if err != nil {
		log.Fatalf("NewRenderStateRowCells: %v", err)
	}
	defer rc.Close()

	row := 0
	for ri.Next() {
		if err := ri.Cells(rc); err != nil {
			log.Fatalf("ri.Cells: %v", err)
		}
		var line []byte
		for rc.Next() {
			cp, err := readCellCodepoint(rc)
			if err != nil {
				log.Fatalf("read codepoint row %d: %v", row, err)
			}
			if cp == 0 {
				line = append(line, ' ')
				continue
			}
			line = append(line, []byte(string(rune(cp)))...)
		}
		fmt.Printf("row %d: %q\n", row, line)
		row++
	}
}

func readCellCodepoint(rc *libghostty.RenderStateRowCells) (uint32, error) {
	cell, err := rc.Raw()
	if err != nil {
		return 0, err
	}
	cp, err := cell.Codepoint()
	if err != nil {
		return 0, err
	}
	return cp, nil
}
