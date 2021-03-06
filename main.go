package main

import (
	"errors"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"
	"unicode"

	"github.com/atotto/clipboard"
	"github.com/xyproto/vt100"
)

const version = "favicon 1.0.0"

func main() {
	var (
		// Color scheme for the "text edit" mode
		defaultEditorForeground      = vt100.LightGreen
		defaultEditorBackground      = vt100.BackgroundDefault
		defaultStatusForeground      = vt100.White
		defaultStatusBackground      = vt100.BackgroundBlack
		defaultStatusErrorForeground = vt100.LightRed
		defaultStatusErrorBackground = vt100.BackgroundDefault
		defaultEditorSearchHighlight = vt100.LightMagenta

		versionFlag = flag.Bool("version", false, "show version information")
		helpFlag    = flag.Bool("help", false, "show simple help")

		statusDuration = 2700 * time.Millisecond

		copyLine   string // for the cut/copy/paste functionality
		statusMode bool   // if information should be shown at the bottom

		clearOnQuit bool // clear the terminal when quitting, or not

		mode Mode // an "enum"/int signalling if this file should be in git mode, markdown mode etc
	)

	flag.Parse()

	if *versionFlag {
		fmt.Println(version)
		return
	}

	if *helpFlag {
		fmt.Println(version + " - simple and limited text editor")
		fmt.Print(`
Hotkeys

ctrl-q     to quit
ctrl-s     to save
ctrl-a     go to start of line, then start of text and then the previous line
ctrl-e     go to end of line and then the next line
ctrl-p     to scroll up 10 lines
ctrl-n     to scroll down 10 lines or go to the next match if a search is active
ctrl-k     to delete characters to the end of the line, then delete the line
ctrl-g     to toggle filename/line/column/unicode/word count status display
ctrl-d     to delete a single character
ctrl-x     to cut the current line
ctrl-c     to copy the current line
ctrl-v     to paste the current line
ctrl-u     to undo
ctrl-l     to jump to a specific line
esc        to redraw the screen and clear the last search
ctrl-space to export to the other image format
ctrl-~     to save and quit + clear the terminal

Set NO_COLOR=1 to disable colors.

`)
		return
	}

	filename := flag.Arg(0)
	if filename == "" {
		fmt.Fprintln(os.Stderr, "Need a filename.")
		os.Exit(1)
	}

	// If the filename ends with "." and the file does not exist, assume this was an attempt at tab-completion gone wrong.
	// If there are multiple files that exist that start with the given filename, open the one first in the alphabet (.cpp before .o)
	if strings.HasSuffix(filename, ".") && !exists(filename) {
		// Glob
		matches, err := filepath.Glob(filename + "*")
		if err == nil && len(matches) > 0 { // no error and at least 1 match
			sort.Strings(matches)
			filename = matches[0]
		}
	}

	baseFilename := filepath.Base(filename)

	// Initialize the terminal
	tty, err := vt100.NewTTY()
	if err != nil {
		fmt.Fprintln(os.Stderr, "error: "+err.Error())
		os.Exit(1)
	}
	defer tty.Close()
	vt100.Init()

	// Check that the file is an .ico or .png image
	if !strings.HasSuffix(filename, ".png") && !strings.HasSuffix(filename, ".ico") {
		quitError(tty, errors.New(filename+" must be an .ico or a .png file"))
	}

	// Create a Canvas for drawing onto the terminal
	c := vt100.NewCanvas()
	c.ShowCursor()

	// scroll 10 lines at a time, no word wrap
	e := NewEditor(defaultEditorForeground, defaultEditorBackground, true, 10, defaultEditorSearchHighlight, mode)

	// Adjust the word wrap if the terminal is too narrow
	w := int(c.Width())
	if w < e.wordWrapAt {
		e.wordWrapAt = w
	}

	// Use a theme for light backgrounds if XTERM_VERSION is set,
	// because $COLORFGBG is "15;0" even though the background is white.
	xterm := os.Getenv("XTERM_VERSION") != ""
	if xterm {
		e.setLightTheme()
	}

	e.respectNoColorEnvironmentVariable()

	status := NewStatusBar(defaultStatusForeground, defaultStatusBackground, defaultStatusErrorForeground, defaultStatusErrorBackground, e, statusDuration)
	status.respectNoColorEnvironmentVariable()

	// Load a file, or a prepare an empty version of the file (without saving it until the user saves it)
	var (
		statusMessage  string
		warningMessage string
	)

	// We wish to redraw the canvas and reposition the cursor
	e.redraw = true
	e.redrawCursor = true

	// Use os.Stat to check if the file exists, and load the file if it does
	if fileInfo, err := os.Stat(filename); err == nil {

		// TODO: Enter file-rename mode when opening a directory?
		// Check if this is a directory
		if fileInfo.IsDir() {
			quitError(tty, errors.New(filename+" is a directory"))
		}

		warningMessage, err = e.Load(c, tty, filename)
		if err != nil {
			quitError(tty, err)
		}

		if !e.Empty() {
			statusMessage = "Loaded " + filename + warningMessage
		} else {
			statusMessage = "Loaded empty file: " + filename + warningMessage
		}

		// Test write, to check if the file can be written or not
		testfile, err := os.OpenFile(filename, os.O_WRONLY, 0664)
		if err != nil {
			// can not open the file for writing
			statusMessage += " (read only)"
			// set the color to red when in read-only mode
			e.fg = vt100.Red
			// do a full reset and redraw
			c = e.FullResetRedraw(c, status)
			// draw the editor lines again
			e.DrawLines(c, false, true)
			e.redraw = false
		}
		testfile.Close()
	} else {
		newMode, err := e.PrepareEmpty(c, tty, filename)
		if err != nil {
			quitError(tty, err)
		}

		statusMessage = "New " + filename

		// For .ico and .png
		if newMode != modeBlank {
			e.mode = newMode
		}

		// Test save, to check if the file can be created and written, or not
		if err := e.Save(&filename, false); err != nil {
			// Check if the new file can be saved before the user starts working on the file.
			quitError(tty, err)
		} else {
			// Creating a new empty file worked out fine, don't save it until the user saves it
			if os.Remove(filename) != nil {
				// This should never happen
				quitError(tty, errors.New("could not remove an empty file that was just created: "+filename))
			}
		}
	}

	// The editing mode is decided at this point

	// Undo buffer with room for 8192 actions
	undo := NewUndo(8192)

	// Resize handler
	SetUpResizeHandler(c, e, status, tty)

	tty.SetTimeout(2 * time.Millisecond)

	previousX := 1
	previousY := 1

	// Draw editor lines from line 0 to h onto the canvas at 0,0
	e.DrawLines(c, false, false)

	status.SetMessage(statusMessage)
	status.Show(c, e)

	if e.redrawCursor {
		x := e.pos.ScreenX()
		y := e.pos.ScreenY()
		previousX = x
		previousY = y
		vt100.SetXY(uint(x), uint(y))
		e.redrawCursor = false
	}

	var (
		quit        bool
		previousKey string
	)

	for !quit {
		key := tty.String()
		switch key {
		case "c:17": // ctrl-q, quit
			quit = true
		case "c:0": // ctrl-space, build source code to executable, word wrap, convert to PDF or write to PNG, depending on the mode
			if strings.HasSuffix(baseFilename, ".ico") {
				// Save .ico as .png
				err := e.Save(&filename, true)
				if err != nil {
					statusMessage = err.Error()
					status.ClearAll(c)
					status.SetMessage(statusMessage)
					status.Show(c, e)
				} else {
					status.ClearAll(c)
					status.SetMessage("Saved " + strings.Replace(baseFilename, ".ico", ".png", 1))
					status.Show(c, e)
				}
				break // from case
			} else if strings.HasSuffix(baseFilename, ".png") {
				// Save .png as .ico
				err := e.Save(&filename, true)
				if err != nil {
					statusMessage = err.Error()
					status.ClearAll(c)
					status.SetMessage(statusMessage)
					status.Show(c, e)
				} else {
					status.ClearAll(c)
					status.SetMessage("Saved " + strings.Replace(baseFilename, ".png", ".ico", 1))
					status.Show(c, e)
				}
				break // from case
			}
			// Building this file extension is not implemented yet.
			status.ClearAll(c)
			// Just display the current time and word count.
			statusMessage := fmt.Sprintf("%d words, %s", e.WordCount(), time.Now().Format("15:04")) // HH:MM
			status.SetMessage(statusMessage)
			status.Show(c, e)
		case "←": // left arrow
			// Draw mode
			e.pos.Left()
			e.redrawCursor = true
		case "→": // right arrow
			// Draw mode
			e.pos.Right(c)
			e.redrawCursor = true
		case "↑": // up arrow
			// Move the screen cursor
			e.pos.Up()
			e.redrawCursor = true
		case "↓": // down arrow
			e.pos.Down(c)
			e.redrawCursor = true
		case "c:14": // ctrl-n, scroll down or jump to next match
			// Scroll down
			e.redraw = e.ScrollDown(c, status, e.pos.scrollSpeed)
			// If e.redraw is false, the end of file is reached
			if !e.redraw {
				status.Clear(c)
				status.SetMessage("EOF")
				status.Show(c, e)
			}
			e.redrawCursor = true
		case "c:16": // ctrl-p, scroll up
			e.redraw = e.ScrollUp(c, status, e.pos.scrollSpeed)
			e.redrawCursor = true
		case "c:27": // esc, clear search term, reset, clean and redraw
			c = e.FullResetRedraw(c, status)
		case " ": // space
			undo.Snapshot(e)
			// Place a space
			e.SetRune(' ')
			e.WriteRune(c)
			e.redraw = true
		case "c:13": // return
			undo.Snapshot(e)
			// if the current line is empty, insert a blank line
			if e.AtLastLineOfDocument() {
				e.CreateLineIfMissing(e.DataY() + 1)
			}
			e.pos.Down(c)
			e.redraw = true
		case "c:8", "c:127": // ctrl-h or backspace
			undo.Snapshot(e)
			// Move back
			e.Prev(c)
			// Type a blank
			e.SetRune(' ')
			e.WriteRune(c)
			e.redrawCursor = true
			e.redraw = true
		case "c:1", "c:25": // ctrl-a, home (or ctrl-y for scrolling up in the st terminal)
			// First check if we just moved to this line with the arrow keys
			justMovedUpOrDown := previousKey == "↓" || previousKey == "↑"
			// If at an empty line, go up one line
			if !justMovedUpOrDown && e.EmptyRightTrimmedLine() {
				e.Up(c, status)
				//e.GoToStartOfTextLine()
				e.End()
			} else if x, err := e.DataX(); err == nil && x == 0 && !justMovedUpOrDown {
				// If at the start of the line,
				// go to the end of the previous line
				e.Up(c, status)
				e.End()
			} else if e.AtStartOfTextLine() {
				// If at the start of the text, go to the start of the line
				e.Home()
			} else {
				// If none of the above, go to the start of the text
				e.GoToStartOfTextLine()
			}
			e.redrawCursor = true
			e.SaveX(true)
		case "c:5": // ctrl-e, end
			// First check if we just moved to this line with the arrow keys
			justMovedUpOrDown := previousKey == "↓" || previousKey == "↑"
			// If we didn't just move here, and are at the end of the line,
			// move down one line and to the end, if not,
			// just move to the end.
			if !justMovedUpOrDown && e.AfterEndOfLine() {
				e.Down(c, status)
				e.End()
			} else {
				e.End()
			}
			e.redrawCursor = true
			e.SaveX(true)
		case "c:4": // ctrl-d, delete
			undo.Snapshot(e)
			if e.Empty() {
				status.SetMessage("Empty")
				status.Show(c, e)
			} else {
				e.Delete()
				e.redraw = true
			}
			e.redrawCursor = true
		case "c:30": // ctrl-~, save and quit + clear the terminal
			clearOnQuit = true
			quit = true
			fallthrough
		case "c:19": // ctrl-s, save
			status.ClearAll(c)
			// Save the file
			if err := e.Save(&filename, false); err != nil {
				status.SetMessage(err.Error())
				status.Show(c, e)
			} else {
				// Status message
				status.SetMessage("Saved " + filename)
				status.Show(c, e)
				c.Draw()
			}
		case "c:21", "c:26": // ctrl-u or ctrl-z, undo (ctrl-z may background the application)
			if err := undo.Restore(e); err == nil {
				//c.Draw()
				x := e.pos.ScreenX()
				y := e.pos.ScreenY()
				vt100.SetXY(uint(x), uint(y))
				e.redrawCursor = true
				e.redraw = true
			} else {
				status.SetMessage("No more to undo")
				status.Show(c, e)
			}
		case "c:12": // ctrl-l, go to line number
			status.ClearAll(c)
			status.SetMessage("Go to line number:")
			status.ShowNoTimeout(c, e)
			lns := ""
			doneCollectingDigits := false
			for !doneCollectingDigits {
				numkey := tty.String()
				switch numkey {
				case "0", "1", "2", "3", "4", "5", "6", "7", "8", "9": // 0 .. 9
					lns += numkey // string('0' + (numkey - 48))
					status.SetMessage("Go to line number: " + lns)
					status.ShowNoTimeout(c, e)
				case "c:8", "c:127": // ctrl-h or backspace
					if len(lns) > 0 {
						lns = lns[:len(lns)-1]
						status.SetMessage("Go to line number: " + lns)
						status.ShowNoTimeout(c, e)
					}
				case "c:27", "c:17": // esc or ctrl-q
					lns = ""
					fallthrough
				case "c:13": // return
					doneCollectingDigits = true
				}
			}
			status.ClearAll(c)
			if lns != "" {
				if ln, err := strconv.Atoi(lns); err == nil { // no error
					e.redraw = e.GoToLineNumber(ln, c, status, true)
				}
			}
			e.redrawCursor = true
		case "c:11": // ctrl-k, delete to end of line
			undo.Snapshot(e)
			if e.Empty() {
				status.SetMessage("Empty")
				status.Show(c, e)
			} else {
				e.DeleteRestOfLine()
				vt100.Do("Erase End of Line")
				e.redraw = true
			}
			e.redrawCursor = true
		case "c:24": // ctrl-x, cut line
			undo.Snapshot(e)
			y := e.DataY()
			copyLine = e.Line(y)
			// Copy the line to the clipboard
			_ = clipboard.WriteAll(copyLine)
			e.DeleteLine(y)
			e.redrawCursor = true
			e.redraw = true
		case "c:3": // ctrl-c, copy the stripped contents of the current line
			trimmed := strings.TrimSpace(e.Line(e.DataY()))
			if trimmed != "" {
				copyLine = trimmed
				// Copy the line to the clipboard
				_ = clipboard.WriteAll(copyLine)
			}
			e.redrawCursor = true
			e.redraw = true
		case "c:22": // ctrl-v, paste
			undo.Snapshot(e)
			// Try fetching the line from the clipboard first
			lines, err := clipboard.ReadAll()
			if err == nil { // no error
				if strings.Contains(lines, "\n") {
					copyLine = strings.SplitN(lines, "\n", 2)[0]
				} else {
					copyLine = lines
				}
			}
			// Fix nonbreaking spaces
			copyLine = strings.Replace(copyLine, string([]byte{0xc2, 0xa0}), string([]byte{0x20}), -1)
			if e.EmptyRightTrimmedLine() {
				// If the line is empty, use the existing indentation before pasting
				e.SetLine(e.DataY(), e.LeadingWhitespace()+strings.TrimSpace(copyLine))
			} else {
				// If the line is not empty, insert the trimmed string
				e.InsertString(c, strings.TrimSpace(copyLine))
			}
			// Prepare to redraw the text
			e.redrawCursor = true
			e.redraw = true
		default:
			if len([]rune(key)) > 0 && unicode.IsLetter([]rune(key)[0]) { // letter
				undo.Snapshot(e)
				// Type the letter that was pressed
				if len([]rune(key)) > 0 {
					// Replace this letter.
					e.SetRune([]rune(key)[0])
					e.WriteRune(c)
					e.redraw = true
				}
			} else if len([]rune(key)) > 0 && unicode.IsGraphic([]rune(key)[0]) { // any other key that can be drawn
				undo.Snapshot(e)

				// Place *something*
				r := []rune(key)[0]

				// "smart dedent"
				if r == '}' || r == ']' || r == ')' {
					lineContents := strings.TrimSpace(e.CurrentLine())
					whitespaceInFront := e.LeadingWhitespace()
					if e.pos.sx > 0 && len(lineContents) == 0 && len(whitespaceInFront) > 0 {
						// move one step left
						e.Prev(c)
						// trim trailing whitespace
						e.TrimRight(e.DataY())
					}
				}

				e.SetRune([]rune(key)[0])
				e.WriteRune(c)
				e.redrawCursor = true
				e.redraw = true
			}
		}
		previousKey = key
		// Redraw, if needed
		if e.redraw {
			// Draw the editor lines on the canvas, respecting the offset
			e.DrawLines(c, true, false)
			e.redraw = false
		} else if e.Changed() {
			c.Draw()
		}
		// Drawing status messages should come after redrawing, but before cursor positioning
		if statusMode {
			status.ShowLineColWordCount(c, e, filename)
		} else if status.isError {
			// Show the status message
			status.Show(c, e)
		}
		// Position the cursor
		x := e.pos.ScreenX()
		y := e.pos.ScreenY()
		if e.redrawCursor || x != previousX || y != previousY {
			vt100.SetXY(uint(x), uint(y))
			e.redrawCursor = false
		}
		previousX = x
		previousY = y
	}

	// Clear all status bar messages
	status.ClearAll(c)

	// Quit everything that has to do with the terminal
	if clearOnQuit {
		vt100.Clear()
		vt100.Close()
	} else {
		c.Draw()
		fmt.Println()
	}
}
