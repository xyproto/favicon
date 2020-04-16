package main

import (
	"bytes"
	"errors"
	"fmt"
	"io/ioutil"
	"strings"
	"unicode"

	"github.com/xyproto/vt100"
)

const (
	// Mode "enum"
	modeBlank = iota
	modeGray4 // for 4-bit grayscale images
	modeRGB   // for 8+8+8 bit RGB images
	modeRGBA  // for 8+8+8+8 bit RGBA images
)

// Mode is a per-filetype mode, like for Markdown
type Mode int

// Editor represents the contents and editor settings, but not settings related to the viewport or scrolling
type Editor struct {
	lines        map[int][]rune       // the contents of the current document
	changed      bool                 // has the contents changed, since last save?
	fg           vt100.AttributeColor // default foreground color
	bg           vt100.AttributeColor // default background color
	drawMode     bool                 // text or draw mode (for ASCII graphics)?
	pos          Position             // the current cursor and scroll position
	searchFg     vt100.AttributeColor // search highlight color
	redraw       bool                 // if the contents should be redrawn in the next loop
	redrawCursor bool                 // if the cursor should be moved to the location it is supposed to be
	gitColor     vt100.AttributeColor // git commit message color
	wordWrapAt   int                  // set to 80 or 100 to trigger word wrap when typing to that column
	mode         Mode                 // a filetype mode, like for git or markdown
}

// NewEditor takes:
// * the number of spaces per tab (typically 2, 4 or 8)
// * foreground color attributes
// * background color attributes
// * if "insert mode" is enabled (as opposed to "draw mode")
func NewEditor(fg, bg vt100.AttributeColor, textEditMode bool, scrollSpeed int, searchFg vt100.AttributeColor, mode Mode) *Editor {
	e := &Editor{}
	e.lines = make(map[int][]rune)
	e.fg = fg
	e.bg = bg
	e.drawMode = !textEditMode
	p := NewPosition(scrollSpeed)
	e.pos = *p
	e.searchFg = searchFg
	// If the file is not to be highlighted, set word wrap to 99 (0 to disable)
	e.wordWrapAt = 99
	e.mode = mode
	return e
}

// setLightTheme sets a theme suitable for white backgrounds
func (e *Editor) setLightTheme() {
	e.fg = vt100.Black
	e.bg = vt100.Gray
	e.searchFg = vt100.Red
	e.gitColor = vt100.Blue
}

// CopyLines will create a new map[int][]rune struct that is the copy of all the lines in the editor
func (e *Editor) CopyLines() map[int][]rune {
	lines2 := make(map[int][]rune)
	for key, runes := range e.lines {
		runes2 := make([]rune, len(runes))
		copy(runes2, runes)
		lines2[key] = runes2
	}
	return lines2
}

// Set will store a rune in the editor data, at the given data coordinates
func (e *Editor) Set(x, y int, r rune) {
	if e.lines == nil {
		e.lines = make(map[int][]rune)
	}
	_, ok := e.lines[y]
	if !ok {
		e.lines[y] = make([]rune, 0, x+1)
	}
	if x < int(len([]rune(e.lines[y]))) {
		e.lines[y][x] = r
		e.changed = true
		return
	}
	// If the line is too short, fill it up with spaces
	for x >= int(len([]rune(e.lines[y]))) {
		e.lines[y] = append(e.lines[y], ' ')
	}
	e.lines[y][x] = r
	e.changed = true
}

// Get will retrieve a rune from the editor data, at the given coordinates
func (e *Editor) Get(x, y int) rune {
	if e.lines == nil {
		return ' '
	}
	runes, ok := e.lines[y]
	if !ok {
		return ' '
	}
	if x >= int(len(runes)) {
		return ' '
	}
	return runes[x]
}

// Changed will return true if the contents were changed since last time this function was called
func (e *Editor) Changed() bool {
	return e.changed
}

// Line returns the contents of line number N, counting from 0
func (e *Editor) Line(n int) string {
	line, ok := e.lines[n]
	if ok {
		var sb strings.Builder
		for _, r := range line {
			sb.WriteRune(r)
		}
		return sb.String()
	}
	return ""
}

// ScreenLine returns the screen contents of line number N, counting from 0
func (e *Editor) ScreenLine(n int) string {
	line, ok := e.lines[n]
	if ok {
		var sb strings.Builder
		for _, r := range line {
			sb.WriteRune(r)
		}
		tabSpace := "\t"
		//return strings.ReplaceAll(sb.String(), "\t", tabSpace)
		return strings.Replace(sb.String(), "\t", tabSpace, -1)
	}
	return ""
}

// LastDataPosition returns the last X index for this line, for the data (does not expand tabs)
// Can be negative, if the line is empty.
func (e *Editor) LastDataPosition(n int) int {
	return len([]rune(e.Line(n))) - 1
}

// LastScreenPosition returns the last X index for this line, for the screen (expands tabs)
// Can be negative, if the line is empty.
func (e *Editor) LastScreenPosition(n int) int {
	return e.LastDataPosition(n)
}

// FirstScreenPosition returns the first X index for this line, that is not whitespace.
func (e *Editor) FirstScreenPosition(n int) int {
	counter := 0
	for _, r := range e.Line(n) {
		if unicode.IsSpace(r) {
			counter++
			continue
		} else {
			break
		}
	}
	return counter
}

// FirstDataPosition returns the first X index for this line, that is not whitespace.
func (e *Editor) FirstDataPosition(n int) int {
	counter := 0
	for _, r := range e.Line(n) {
		if !unicode.IsSpace(r) {
			break
		}
		counter++
	}
	return counter
}

// Count the number of instances of the rune r in the line n
func (e *Editor) Count(r rune, n int) int {
	var counter int
	line, ok := e.lines[n]
	if ok {
		for _, l := range line {
			if l == r {
				counter++
			}
		}
	}
	return counter
}

// Len returns the number of lines
func (e *Editor) Len() int {
	maxy := 0
	for y := range e.lines {
		if y > maxy {
			maxy = y
		}
	}
	return maxy + 1
}

// String returns the contents of the editor
func (e *Editor) String() string {
	var sb strings.Builder
	for i := 0; i < e.Len(); i++ {
		sb.WriteString(e.Line(i) + "\n")
	}
	//return strings.TrimRight(sb.String(), " \n\t\r")
	return sb.String()
}

// Clear removes all data from the editor
func (e *Editor) Clear() {
	e.lines = make(map[int][]rune)
	e.changed = true
}

// Load will try to load a file. The file is assumed to be checked to already exist.
// Returns a warning message (possibly empty) and an error type
func (e *Editor) Load(c *vt100.Canvas, tty *vt100.TTY, filename string) (string, error) {

	var message string

	var (
		mode Mode
		data []byte
		err  error
	)

	// TODO: Use a lookup table from file extension to read function and editor settings function
	// Read the file
	if strings.HasSuffix(filename, ".ico") {
		// Try to read the file
		mode, data, message, err = ReadFavicon(filename, false, false)
		if err == nil { // no error
			e.mode = mode
			e.drawMode = true
		}
	} else if strings.HasSuffix(filename, ".png") {
		// Try to read the file
		mode, data, message, err = ReadFavicon(filename, false, true)
		if err == nil { // no error
			e.mode = mode
			e.drawMode = true
		}
	} else {
		// Any other file extension
		data, err = ioutil.ReadFile(filename)
		if bytes.Contains(data, []byte{'\r'}) {
			// Replace DOS line endings with UNIX line endings
			data = bytes.Replace(data, []byte{'\r', '\n'}, []byte{'\n'}, -1)
			// Replace any remaining \r characters with \n
			data = bytes.Replace(data, []byte{'\r'}, []byte{'\n'}, -1)
		}
	}

	// Check if the file could be read
	if err != nil {
		return message, err
	}

	datalines := bytes.Split(data, []byte{'\n'})
	e.Clear()
	for y, dataline := range datalines {
		line := string(dataline)
		counter := 0
		for _, letter := range line {
			e.Set(counter, int(y), letter)
			counter++
		}
	}
	// Mark the data as "not changed"
	e.changed = false

	return message, nil
}

// PrepareEmpty prepares an empty textual representation of a given filename.
// If it's an image, there will be text placeholders for pixels.
// If it's anything else, it will just be blank.
// Returns an editor mode and an error type.
func (e *Editor) PrepareEmpty(c *vt100.Canvas, tty *vt100.TTY, filename string) (Mode, error) {
	var (
		mode Mode = modeBlank
		data []byte
		err  error
	)

	// Prepare the file
	if strings.HasSuffix(filename, ".ico") {
		// Create empty content
		mode, data, _, err = ReadFavicon(filename, true, false)
		if err == nil { // no error
			e.drawMode = true
		}
	} else if strings.HasSuffix(filename, ".png") {
		// Create empty content
		mode, data, _, err = ReadFavicon(filename, true, true)
		if err == nil { // no error
			e.drawMode = true
		}
	}

	// For any other extension, data is left empty and mode is left as blankMode

	// Check if the data could be prepared
	if err != nil {
		return mode, err
	}

	datalines := bytes.Split(data, []byte{'\n'})
	e.Clear()
	for y, dataline := range datalines {
		line := string(dataline)
		counter := 0
		for _, letter := range line {
			e.Set(counter, int(y), letter)
			counter++
		}
	}
	// Mark the data as "not changed"
	e.changed = false

	return mode, nil
}

// Save will try to save a file
// if asOther is true, .ico files will be saved as .png, and .png files will be saved as .ico
func (e *Editor) Save(filename *string, asOther bool) error {
	stripTrailingSpaces := true
	if strings.HasSuffix(*filename, ".ico") || strings.HasSuffix(*filename, ".png") {
		// TODO: Find a way to check if the file was written with "o".
		//       If it was not, save to a new flename.
		// Save the image as .ico if this is a .png file and asOther is true
		// Save the image as .png if this is a .ico file and asOther is true
		// If asOther is false, save as the same filename
		// TODO: Find a cleaner API
		return WriteFavicon(e.mode, e.String(), *filename, asOther)
	}
	var data []byte
	if stripTrailingSpaces {
		// Strip trailing spaces
		for i := 0; i < e.Len(); i++ {
			e.TrimRight(i)
		}
		// Skip trailing newlines
		data = bytes.TrimRightFunc([]byte(e.String()), unicode.IsSpace)
		// Replace nonbreaking space with regular spaces
		data = bytes.Replace(data, []byte{0xc2, 0xa0}, []byte{0x20}, -1)
		// Add a final newline
		data = append(data, '\n')
	} else {
		data = []byte(e.String())
	}
	// Mark the data as "not changed"
	e.changed = false
	// Write the data to file
	return ioutil.WriteFile(*filename, data, 0664)
}

// TrimRight will remove whitespace from the end of the given line number
func (e *Editor) TrimRight(n int) {
	if _, ok := e.lines[n]; !ok {
		return
	}
	lastIndex := len([]rune(e.lines[n])) - 1
	// find the last non-space position
	for x := lastIndex; x >= 0; x-- {
		if !unicode.IsSpace(e.lines[n][x]) {
			lastIndex = x
			break
		}
	}
	// Remove the trailing spaces
	e.lines[n] = e.lines[n][:(lastIndex + 1)]
	e.changed = true
}

// WriteLines will draw editor lines from "fromline" to and up to "toline" to the canvas, at cx, cy
func (e *Editor) WriteLines(c *vt100.Canvas, fromline, toline, cx, cy int) error {
	tabString := " "
	w := int(c.Width())
	if fromline >= toline {
		return errors.New("fromline >= toline in WriteLines")
	}
	numlines := toline - fromline
	offset := fromline
	for y := 0; y < numlines; y++ {
		counter := 0
		//line := strings.ReplaceAll(e.Line(y+offset), "\t", tabString)
		line := strings.Replace(e.Line(y+offset), "\t", tabString, -1)
		screenLine := strings.TrimRightFunc(line, unicode.IsSpace)
		if len([]rune(screenLine)) >= w {
			screenLine = screenLine[:w]
		}
		// Output a regular line
		c.Write(uint(cx+counter), uint(cy+y), e.fg, e.bg, screenLine)
		counter += len([]rune(screenLine))
		// Fill the rest of the line on the canvas with "blanks"
		for x := counter; x < w; x++ {
			c.WriteRune(uint(cx+x), uint(cy+y), e.fg, e.bg, ' ')
		}
	}
	return nil
}

// DeleteRestOfLine will delete the rest of the line, from the given position
func (e *Editor) DeleteRestOfLine() {
	x, err := e.DataX()
	if err != nil {
		// position is after the data, do nothing
		return
	}
	y := e.DataY()
	if e.lines == nil {
		e.lines = make(map[int][]rune)
	}
	_, ok := e.lines[y]
	if !ok {
		return
	}
	if x >= len([]rune(e.lines[y])) {
		return
	}
	e.lines[y] = e.lines[y][:x]
	e.changed = true
}

// DeleteLine will delete the given line index
func (e *Editor) DeleteLine(n int) {
	endOfDocument := n >= (e.Len() - 1)
	if endOfDocument {
		// Just delete this line
		delete(e.lines, n)
		return
	}
	// Shift all lines after y so that y is overwritten.
	// Then delete the last item.
	maxIndex := 0
	found := false
	for k := range e.lines {
		if k > maxIndex {
			maxIndex = k
			found = true
		}
	}
	if !found {
		// This should never happen
		return
	}
	if _, ok := e.lines[maxIndex]; !ok {
		// The line numbers and the length of e.lines does not match
		return
	}
	// Shift all lines after n one step closer to n, overwriting e.lines[n]
	for i := n; i <= (maxIndex - 1); i++ {
		e.lines[i] = e.lines[i+1]
	}
	// delete the final item
	delete(e.lines, maxIndex)

	e.changed = true

	// Make sure no lines are nil
	e.MakeConsistent()
}

// Delete will delete a character at the given position
func (e *Editor) Delete() {
	y := e.DataY()
	llen := len([]rune(e.lines[y]))
	if _, ok := e.lines[y]; !ok || llen == 0 || llen == 1 && unicode.IsSpace(e.lines[y][0]) {
		// All keys in the map that are > y should be shifted -1.
		// This also overwrites e.lines[y].
		e.DeleteLine(y)
		e.changed = true
		return
	}
	x, err := e.DataX()
	if err != nil || x >= len([]rune(e.lines[y]))-1 {
		// on the last index, just use every element but x
		e.lines[y] = e.lines[y][:x]
		// check if the next line exists
		if _, ok := e.lines[y+1]; ok {
			// then add the contents of the next line, if available
			nextLine, ok := e.lines[y+1]
			if ok && len([]rune(nextLine)) > 0 {
				e.lines[y] = append(e.lines[y], nextLine...)
				// then delete the next line
				e.DeleteLine(y + 1)
			}
		}
		e.changed = true
		return
	}
	// Delete just this character
	e.lines[y] = append(e.lines[y][:x], e.lines[y][x+1:]...)

	e.changed = true

	// Make sure no lines are nil
	e.MakeConsistent()
}

// Empty will check if the current editor contents are empty or not.
// If there's only one line left and it is only whitespace, that will be considered empty as well.
func (e *Editor) Empty() bool {
	l := len(e.lines)
	switch l {
	case 0:
		return true
	case 1:
		// Check the contents of the 1 remaining line,
		// without specifying a key.
		for _, v := range e.lines {
			if len(strings.TrimSpace(string(v))) == 0 {
				return true
			}
			break
		}
		fallthrough
	default:
		// > 1 lines
		return false
	}
}

// MakeConsistent creates an empty slice of runes for any empty lines,
// to make sure that no line number below e.Len() points to a nil map.
func (e *Editor) MakeConsistent() {
	// Check if the keys in the map are consistent
	for i := 0; i < len(e.lines); i++ {
		if _, found := e.lines[i]; !found {
			e.lines[i] = make([]rune, 0)
			e.changed = true
		}
	}
}

// WithinLimit will check if a line is within the word wrap limit,
// given a Y position.
func (e *Editor) WithinLimit(y int) bool {
	return len(e.lines[y]) < e.wordWrapAt
}

// LastWord will return the last word of a line,
// given a Y position. Returns an empty string if there is no last word.
func (e *Editor) LastWord(y int) string {
	// TODO: Use a faster method
	words := strings.Fields(strings.TrimSpace(string(e.lines[y])))
	if len(words) > 0 {
		return words[len(words)-1]
	}
	return ""
}

// SplitOvershoot will split the line into a first part that is within the
// word wrap length and a second part that is the overshooting part.
// y is the line index (y position, counting from 0).
// isSpace is true if a space has just been inserted on purpose at the current position.
func (e *Editor) SplitOvershoot(y int, isSpace bool) ([]rune, []rune) {

	// Maximum word length to not keep as one word
	maxDistance := e.wordWrapAt / 2
	if e.WithinLimit(y) {
		return e.lines[y], []rune{}
	}
	splitPosition := e.wordWrapAt
	if isSpace {
		splitPosition, _ = e.DataX()
	} else {
		// Starting at the split position, move left until a space is reached (or the start of the line).
		// If a space is reached, check if it is too far away from n to be used as a split position, or not.
		spacePosition := -1
		for i := splitPosition; i >= 0; i-- {
			if i < len(e.lines[y]) && unicode.IsSpace(e.lines[y][i]) {
				// Found a space at position i
				spacePosition = i
				break
			}
		}
		// Found a better position to split, at a nearby space?
		if spacePosition != -1 {
			distance := splitPosition - spacePosition
			if distance > maxDistance {
				// To far away, don't use this as a split point,
				// stick to the hard split.
			} else {
				// Okay, we found a better split point.
				splitPosition = spacePosition
			}
		}
	}

	// Split the line into two parts

	n := splitPosition
	// Make space for the two parts
	first := make([]rune, len(e.lines[y][:n]))
	second := make([]rune, len(e.lines[y][n:]))
	// Copy the line into first and second
	copy(first, e.lines[y][:n])
	copy(second, e.lines[y][n:])

	// If the second part starts with a space, remove it
	if len(second) > 0 && unicode.IsSpace(second[0]) {
		second = second[1:]
	}

	return first, second
}

// WrapAllLinesAt will word wrap all lines that are longer than n,
// with a maximum overshoot of too long words (measured in runes) of maxOvershoot.
// Returns true if any lines were wrapped.
func (e *Editor) WrapAllLinesAt(n, maxOvershoot int) bool {
	// This is not even called when the problematic insert behavior occurs

	wrapped := false
	for i := 0; i < e.Len(); i++ {
		if e.WithinLimit(i) {
			continue
		}
		wrapped = true

		first, second := e.SplitOvershoot(i, false)

		if len(first) > 0 && len(second) > 0 {

			e.InsertLineBelowAt(i)
			e.lines[i] = first
			e.lines[i+1] = second

			e.changed = true

			// Move the cursor as well, so that it is at the same line as before the word wrap
			if i < e.DataY() {
				e.pos.sy++
			}
		}
	}
	return wrapped
}

// InsertLineAbove will attempt to insert a new line above the current position
func (e *Editor) InsertLineAbove() {
	y := e.DataY()

	// Create new set of lines
	lines2 := make(map[int][]rune)

	// For each line in the old map, if at (y-1), insert a blank line
	// (insert a blank line above)
	for k, v := range e.lines {
		if k < (y - 1) {
			lines2[k] = v
		} else if k == (y - 1) {
			lines2[k] = v
			lines2[k+1] = make([]rune, 0)
		} else if k > (y - 1) {
			lines2[k+1] = v
		}
	}
	// Use the new set of lines
	e.lines = lines2

	// Make sure no lines are nil
	e.MakeConsistent()

	// Skip trailing newlines after this line
	for i := len(e.lines); i > y; i-- {
		if len([]rune(e.lines[i])) == 0 {
			delete(e.lines, i)
		} else {
			break
		}
	}
	e.changed = true

	// Make sure no lines are nil
	e.MakeConsistent()
}

// InsertLineBelow will attempt to insert a new line below the current position
func (e *Editor) InsertLineBelow() {
	e.InsertLineBelowAt(e.DataY())
}

// InsertLineBelowAt will attempt to insert a new line below the given y position
func (e *Editor) InsertLineBelowAt(y int) {
	// Make sure no lines are nil
	e.MakeConsistent()

	// If we are the the last line, add an empty line at the end and return
	if y == (len(e.lines) - 1) {
		e.lines[y+1] = make([]rune, 0)
		e.changed = true
		return
	}

	// Create new set of lines, with room for one more
	lines2 := make(map[int][]rune, len(e.lines)+1)

	// For each line in the old map, if at y, insert a blank line
	// (insert a blank line below)
	for k, v := range e.lines {
		if k < y {
			lines2[k] = v
		} else if k == y {
			lines2[k] = v
			lines2[k+1] = make([]rune, 0)
		} else if k > y {
			lines2[k+1] = v
		}
	}
	// Use the new set of lines
	e.lines = lines2

	// Make sure no lines are nil
	e.MakeConsistent()

	// Skip trailing newlines after this line
	for i := len(e.lines); i > y; i-- {
		if len([]rune(e.lines[i])) == 0 {
			delete(e.lines, i)
		} else {
			break
		}
	}

	e.changed = true

	// Make sure no lines are nil
	e.MakeConsistent()
}

// Insert will insert a rune at the given position, with no word wrap,
// but MakeConsisten will be called.
func (e *Editor) Insert(r rune) {
	// Ignore it if the current position is out of bounds
	x, _ := e.DataX()

	y := e.DataY()

	// If there are no lines, initialize and set the 0th rune to the given one
	if e.lines == nil {
		e.lines = make(map[int][]rune)
		e.lines[0] = []rune{r}
		return
	}

	// If the current line is empty, initialize it with a line that is just the given rune
	_, ok := e.lines[y]
	if !ok {
		e.lines[y] = []rune{r}
		return
	}
	if len([]rune(e.lines[y])) < x {
		// Can only insert in the existing block of text
		return
	}
	newlineLength := len(e.lines[y]) + 1
	newline := make([]rune, newlineLength)
	for i := 0; i < x; i++ {
		newline[i] = e.lines[y][i]
	}
	newline[x] = r
	for i := x + 1; i < newlineLength; i++ {
		newline[i] = e.lines[y][i-1]
	}
	e.lines[y] = newline

	e.changed = true

	// Make sure no lines are nil
	e.MakeConsistent()
}

// CreateLineIfMissing will create a line at the given Y index, if it's missing
func (e *Editor) CreateLineIfMissing(n int) {
	if e.lines == nil {
		e.lines = make(map[int][]rune)
	}
	_, ok := e.lines[n]
	if !ok {
		e.lines[n] = make([]rune, 0)
		e.changed = true
	}
	// Make sure no lines are nil
	e.MakeConsistent()
}

// SetColors will set the current editor theme (foreground, background).
// The background color should be a background attribute (like vt100.BackgroundBlue).
func (e *Editor) SetColors(fg, bg vt100.AttributeColor) {
	e.fg = fg
	e.bg = bg
}

// WordCount returns the number of spaces in the text + 1
func (e *Editor) WordCount() int {
	return len(strings.Fields(e.String()))
}

// SetLine will fill the given line index with the given string.
// Any previous contents of that line is removed.
func (e *Editor) SetLine(n int, s string) {
	e.CreateLineIfMissing(n)
	e.lines[n] = []rune{}
	counter := 0
	// It's important not to use the index value when looping over a string,
	// unless the byte index is what one's after, as opposed to the rune index.
	for _, letter := range s {
		e.Set(counter, n, letter)
		counter++
	}
}

// SplitLine will, at the given position, split the line in two.
// The right side of the contents is moved to a new line below.
func (e *Editor) SplitLine() bool {
	x, err := e.DataX()
	if err != nil {
		// After contents, this should not happen, do nothing
		return false
	}

	y := e.DataY()

	// Get the contents of this line
	runeLine := e.lines[y]
	if len(runeLine) < 2 {
		// Did not split
		return false
	}
	leftContents := strings.TrimRightFunc(string(runeLine[:x]), unicode.IsSpace)
	rightContents := string(runeLine[x:])
	// Insert a new line above this one
	e.InsertLineAbove()
	// Replace this line with the left contents
	e.SetLine(y, leftContents)
	e.SetLine(y+1, rightContents)
	// Splitted
	return true
}

// DataX will return the X position in the data (as opposed to the X position in the viewport)
func (e *Editor) DataX() (int, error) {
	if e.drawMode {
		return e.pos.sx, nil
	}
	// the y position in the data is the lines scrolled + current screen cursor Y position
	dataY := e.pos.offset + e.pos.sy
	// get the current line of text
	screenCounter := 0 // counter for the characters on the screen
	// loop, while also keeping track of tab expansion
	// add a space to allow to jump to the position after the line and get a valid data position
	found := false
	dataX := 0
	runeCounter := 0
	for range e.lines[dataY] {
		// When we reached the correct screen position, use i as the data position
		if screenCounter == e.pos.sx {
			dataX = runeCounter
			found = true
			break
		}
		// Increase the counter
		screenCounter++
		runeCounter++
	}
	if !found {
		return runeCounter, errors.New("position is after data")
	}
	// Return the data cursor
	return dataX, nil
}

// DataY will return the Y position in the data (as opposed to the Y position in the viewport)
func (e *Editor) DataY() int {
	if e.drawMode {
		return e.pos.sy
	}
	return e.pos.offset + e.pos.sy
}

// SetRune will set a rune at the current data position
func (e *Editor) SetRune(r rune) {
	// Only set a rune if x is within the current line contents
	if x, err := e.DataX(); err == nil {
		e.Set(x, e.DataY(), r)
	}
}

// nextLine will go to the start of the next line
func (e *Editor) nextLine(y int, c *vt100.Canvas, status *StatusBar) {
	e.pos.sx = 0
	e.GoTo(y+1, c, status)
}

// insertBelow will insert the given rune at the start of the line below,
// starting a new line if required.
func (e *Editor) insertBelow(y int, r rune) {
	if _, ok := e.lines[y+1]; !ok {
		// If the next line does not exist, create one containing just "r"
		e.lines[y+1] = []rune{r}
	} else if len(e.lines[y+1]) > 0 {
		// If the next line is non-empty, insert "r" at the start
		e.lines[y+1] = append([]rune{r}, e.lines[y+1][:]...)
	} else {
		// The next line exists, but is of length 0, should not happen, just replace it
		e.lines[y+1] = []rune{r}
	}
}

// InsertRune will insert a rune at the current data position, with word wrap
func (e *Editor) InsertRune(c *vt100.Canvas, r rune) {
	y := e.DataY()

	// If it's not a word-wrap situation, just insert and return
	if e.wordWrapAt == 0 || e.WithinLimit(y) {
		e.Insert(r)
		return
	}

	// --- Repaint, afterwards ---

	e.changed = true
	e.redrawCursor = true
	e.redraw = true

	// --- Gather some facts ---

	isSpace := unicode.IsSpace(r)
	x, err := e.DataX()
	if err != nil {
		x = e.pos.sx
	}
	prevAtSpace := false
	if x > 0 && x <= len(e.lines[y]) {
		prevAtSpace = unicode.IsSpace(e.lines[y][x-1])
	}
	atSpace := false
	if x >= 0 && x < len(e.lines[y]) {
		atSpace = unicode.IsSpace(e.lines[y][x])
	}
	//panic(fmt.Sprintf("x=%d, y=%d, line=%s, atSpace=%v, prevAtSpace=%v\n", x, y, e.Line(y), atSpace, prevAtSpace))
	EOL := e.AtOrAfterEndOfLine()

	lastWord := []rune(strings.TrimSpace(e.LastWord(y)))
	shortWord := (len(string(lastWord)) < 10) && (len(string(lastWord)) < e.wordWrapAt)

	//s := fmt.Sprintf("InsertRune, isSpace=%v, atSpace=%v, prevAtSpace=%v, EOL=%v, r=%s, lastWord=%s, shortWord=%v\n", isSpace, atSpace, prevAtSpace, EOL, string(r), string(lastWord), shortWord)
	//logf(s)

	// --- A large switch/case for catching all cases ---

	switch {
	case !EOL:
		// The line is full. Move everything one line down and continue writing.
		right := e.lines[y][x:]
		e.lines[y] = e.lines[y][:x]
		e.TrimRight(y)
		e.insertBelow(y, r)
		e.lines[y+1] = append([]rune{r}, right...)
		// Go to the len(lastWord)-1 of the next line
		e.GoTo(y+1, c, nil)
		e.pos.sx = 0
	case !isSpace && !atSpace && EOL:
		// Pressing letters, producing a short word that overflows
		lastWord = append(lastWord, r)
		// Remove the last r of the current line
		pos := len(e.lines[y]) - len(lastWord)
		if pos > 0 {
			e.lines[y] = e.lines[y][:pos]
			e.TrimRight(y)
		} else {
			// This would leave the current line empty!
			// Typing a letter at the end of a line, breaking a word
			if _, ok := e.lines[y+1]; !ok {
				// If the next line does not exist, create one containing just "r"
				e.lines[y+1] = []rune{r}
			} else if len(e.lines[y+1]) > 0 {
				// If the next line is non-empty, insert "r" at the start
				e.lines[y+1] = append([]rune{r}, e.lines[y+1][:]...)
			}
			// Go to the start of the next line
			e.nextLine(y, c, nil)
			break
		}
		// Insert the last word of the above line on the next line
		if _, ok := e.lines[y+1]; !ok {
			// If the next line does not exist, create one containing just "lastWord" + "r"
			if prevAtSpace {
				lastpos := len(lastWord) - 1
				lastWord = append(lastWord[:lastpos], ' ')
				lastWord = append(lastWord, r)
			}
			e.lines[y+1] = lastWord
		} else if len(e.lines[y+1]) > 0 {
			// If the next line is non-empty, insert "lastWord" + "r" at the start
			e.lines[y+1] = append(lastWord, e.lines[y+1][:]...)
		}
		// Go to the len(lastWord)-1 of the next line
		e.GoTo(y+1, c, nil)
		e.pos.sx = len(lastWord) - 1
	case isSpace && EOL:
		// Space at the end of a long word
		e.InsertLineBelowAt(y)
	case !isSpace && EOL && !shortWord:
		fallthrough
	case !isSpace && prevAtSpace && EOL:
		fallthrough
	case !isSpace && atSpace && !prevAtSpace && EOL && shortWord:
		fallthrough
	case !isSpace && !atSpace && !prevAtSpace && EOL && !shortWord:
		// Pressing a single letter
		e.insertBelow(y, r)
		e.nextLine(y, c, nil)
	case !isSpace && !atSpace && !prevAtSpace && EOL && shortWord:
		// Typing a letter or a space, and the word is short, so it should be moved down
		if !isSpace {
			lastWord = append(lastWord, r)
		}
		// Remove the last r of the current line
		pos := len(e.lines[y]) - len(lastWord)
		if pos > 0 {
			e.lines[y] = e.lines[y][:pos]
			e.TrimRight(y)
		} else {
			// This would leave the current line empty!
			// Typing a letter at the end of a line, breaking a word
			if _, ok := e.lines[y+1]; !ok {
				// If the next line does not exist, create one containing just "r"
				e.lines[y+1] = []rune{r}
			} else if len(e.lines[y+1]) > 0 {
				// If the next line is non-empty, insert "r" at the start
				e.lines[y+1] = append([]rune{r}, e.lines[y+1][:]...)
			}
			// Go to the start of the next line
			e.nextLine(y, c, nil)
			break
		}
		// Insert the last word of the above line on the next line
		if _, ok := e.lines[y+1]; !ok {
			// If the next line does not exist, create one containing just "lastWord" + "r"
			if prevAtSpace {
				lastpos := len(lastWord) - 1
				lastWord = append(lastWord[:lastpos], ' ')
				lastWord = append(lastWord, r)
			}
			e.lines[y+1] = lastWord
		} else if len(e.lines[y+1]) > 0 {
			// If the next line is non-empty, insert "lastWord" + "r" at the start
			e.lines[y+1] = append(lastWord, e.lines[y+1][:]...)
		}
		// Go to the len(lastWord)-1 of the next line
		e.GoTo(y+1, c, nil)
		e.pos.sx = len(lastWord) - 1
	default:
		e.Insert(r)
	}
	e.TrimRight(y)
	e.MakeConsistent()
}

// InsertString will insert a string at the current data position.
// This will also call e.WriteRune and e.Next, as needed.
func (e *Editor) InsertString(c *vt100.Canvas, s string) {
	for _, r := range s {
		e.InsertRune(c, r)
		e.WriteRune(c)
		e.Next(c)
	}
}

// Rune will get the rune at the current data position
func (e *Editor) Rune() rune {
	x, err := e.DataX()
	if err != nil {
		// after line contents, return a zero rune
		return rune(0)
	}
	return e.Get(x, e.DataY())
}

// CurrentLine will get the current data line, as a string
func (e *Editor) CurrentLine() string {
	return e.Line(e.DataY())
}

// Home will move the cursor the the start of the line (x = 0)
func (e *Editor) Home() {
	e.pos.sx = 0
}

// End will move the cursor to the position right after the end of the current line contents,
// and also trim away whitespace from the right side.
func (e *Editor) End() {
	y := e.DataY()
	e.TrimRight(y)
	e.pos.sx = e.LastScreenPosition(y) + 1
}

// AtEndOfLine returns true if the cursor is at exactly the last character of the line, not the one after
func (e *Editor) AtEndOfLine() bool {
	return e.pos.sx == e.LastScreenPosition(e.DataY())
}

// DownEnd will move down and then choose a "smart" X position
func (e *Editor) DownEnd(c *vt100.Canvas) error {
	tmpx := e.pos.sx
	err := e.pos.Down(c)
	if err != nil {
		return err
	}
	if len(strings.TrimSpace(e.CurrentLine())) == 1 {
		e.TrimRight(e.DataY())
		e.End()
	} else if e.AfterLineScreenContentsPlusOne() && tmpx > 1 {
		e.End()
		if e.pos.sx != tmpx && e.pos.sx > e.pos.savedX {
			e.pos.savedX = tmpx
		}
	} else {
		e.pos.sx = e.pos.savedX
		// Also checking if e.Rune() is ' ' is nice for code, but horrible for regular text files
		if e.Rune() == '\t' {
			e.pos.sx = e.FirstScreenPosition(e.DataY())
		}
	}
	return nil
}

// UpEnd will move up and then choose a "smart" X position
func (e *Editor) UpEnd(c *vt100.Canvas) error {
	tmpx := e.pos.sx
	err := e.pos.Up()
	if err != nil {
		return err
	}
	if e.AfterLineScreenContentsPlusOne() && tmpx > 1 {
		e.End()
		if e.pos.sx != tmpx && e.pos.sx > e.pos.savedX {
			e.pos.savedX = tmpx
		}
	} else {
		e.pos.sx = e.pos.savedX
		// Also checking if e.Rune() is ' ' is nice for code, but horrible for regular text files
		if e.Rune() == '\t' {
			e.pos.sx = e.FirstScreenPosition(e.DataY())
		}
	}
	return nil
}

// Next will move the cursor to the next position in the contents
func (e *Editor) Next(c *vt100.Canvas) error {
	e.pos.sx++
	// Did we move too far on this line?
	w := e.wordWrapAt
	if c != nil {
		w = int(c.W())
	}
	if e.pos.sx >= w {
		// Undo the move
		e.pos.sx--
	}
	return nil
}

// Prev will move the cursor to the previous position in the contents
func (e *Editor) Prev(c *vt100.Canvas) error {
	e.pos.sx--
	// Did we move too far?
	if e.pos.sx < 0 {
		// Undo the move
		e.pos.sx++
	}
	return nil
}

// Right will move the cursor to the right, if possible.
// It will not move the cursor up or down.
func (p *Position) Right(c *vt100.Canvas) {
	lastX := int(c.Width() - 1)
	if p.sx < lastX {
		p.sx++
	}
}

// Left will move the cursor to the left, if possible.
// It will not move the cursor up or down.
func (p *Position) Left() {
	if p.sx > 0 {
		p.sx--
	}
}

// SaveX will save the current X position, if it's within reason
func (e *Editor) SaveX(regardless bool) {
	if regardless || (!e.AfterLineScreenContentsPlusOne() && e.pos.sx > 1) {
		e.pos.savedX = e.pos.sx
	}
}

// ScrollDown will scroll down the given amount of lines given in scrollSpeed
func (e *Editor) ScrollDown(c *vt100.Canvas, status *StatusBar, scrollSpeed int) bool {
	// Find out if we can scroll scrollSpeed, or less
	canScroll := scrollSpeed
	// last y position in the canvas
	canvasLastY := int(c.H() - 1)
	// number of lines in the document
	l := e.Len()
	if e.pos.offset >= e.Len()-canvasLastY {
		// Status message
		//status.SetMessage("End of text")
		//status.Show(c, p)
		c.Draw()
		// Don't redraw
		return false
	}
	status.Clear(c)
	if (e.pos.offset + canScroll) >= (l - canvasLastY) {
		// Almost at the bottom, we can scroll the remaining lines
		canScroll = (l - canvasLastY) - e.pos.offset
	}
	// Move the scroll offset
	e.pos.offset += canScroll
	// Prepare to redraw
	return true
}

// ScrollUp will scroll down the given amount of lines given in scrollSpeed
func (e *Editor) ScrollUp(c *vt100.Canvas, status *StatusBar, scrollSpeed int) bool {
	// Find out if we can scroll scrollSpeed, or less
	canScroll := scrollSpeed
	if e.pos.offset == 0 {
		// Can't scroll further up
		// Status message
		//status.SetMessage("Start of text")
		//status.Show(c, p)
		c.Draw()
		// Don't redraw
		return false
	}
	status.Clear(c)
	if e.pos.offset-canScroll < 0 {
		// Almost at the top, we can scroll the remaining lines
		canScroll = e.pos.offset
	}
	// Move the scroll offset
	e.pos.offset -= canScroll
	// Prepare to redraw
	return true
}

// AtLastLineOfDocument is true if we're at the last line of the document (or beyond)
func (e *Editor) AtLastLineOfDocument() bool {
	return e.DataY() >= (e.Len() - 1)
}

// AfterLastLineOfDocument is true if we're after the last line of the document (or beyond)
func (e *Editor) AfterLastLineOfDocument() bool {
	return e.DataY() > (e.Len() - 1)
}

// AtOrAfterEndOfDocument is true if the cursor is at or after the end of the last line of the document
func (e *Editor) AtOrAfterEndOfDocument() bool {
	return e.AtLastLineOfDocument() && e.AtOrAfterEndOfLine()
}

// AfterEndOfDocument is true if the cursor is after the end of the last line of the document
func (e *Editor) AfterEndOfDocument() bool {
	return e.AfterLastLineOfDocument() && e.AtOrAfterEndOfLine()
}

// AtEndOfDocument is true if the cursor is at the end of the last line of the document
func (e *Editor) AtEndOfDocument() bool {
	return e.AtLastLineOfDocument() && e.AtEndOfLine()
}

// AtStartOfDocument is true if we're at the first line of the document
func (e *Editor) AtStartOfDocument() bool {
	return e.pos.sy == 0 && e.pos.offset == 0
}

// AtOrAfterEndOfLine returns true if the cursor is at or after the contents of this line
func (e *Editor) AtOrAfterEndOfLine() bool {
	x, err := e.DataX()
	if err != nil {
		// After end of data
		return true
	}
	return x >= e.LastDataPosition(e.DataY())
}

// AfterEndOfLine returns true if the cursor is after the contents of this line
func (e *Editor) AfterEndOfLine() bool {
	x, err := e.DataX()
	if err != nil {
		// After end of data
		return true
	}
	return x > e.LastDataPosition(e.DataY())
}

// AfterLineScreenContents will check if the cursor is after the current line contents
func (e *Editor) AfterLineScreenContents() bool {
	return e.pos.sx > e.LastScreenPosition(e.DataY())
}

// AfterLineScreenContentsPlusOne will check if the cursor is after the current line contents, with a margin of 1
func (e *Editor) AfterLineScreenContentsPlusOne() bool {
	return e.pos.sx > (e.LastScreenPosition(e.DataY()) + 1)
}

// WriteRune writes the current rune to the given canvas
func (e *Editor) WriteRune(c *vt100.Canvas) {
	if c != nil {
		c.WriteRune(uint(e.pos.sx), uint(e.pos.sy), e.fg, e.bg, e.Rune())
	}
}

// EmptyRightTrimmedLine checks if the current line is empty (and whitespace doesn't count)
func (e *Editor) EmptyRightTrimmedLine() bool {
	return len(strings.TrimRightFunc(e.CurrentLine(), unicode.IsSpace)) == 0
}

// EmptyLine returns true if the line is completely empty, no whitespace or anything
func (e *Editor) EmptyLine() bool {
	return e.CurrentLine() == ""
}

// AtStartOfTextLine returns true if the position is at the start of the text for this line
func (e *Editor) AtStartOfTextLine() bool {
	return e.pos.sx == e.FirstScreenPosition(e.DataY())
}

// BeforeStartOfTextLine returns true if the position is before the start of the text for this line
func (e *Editor) BeforeStartOfTextLine() bool {
	return e.pos.sx < e.FirstScreenPosition(e.DataY())
}

// AtOrBeforeStartOfTextLine returns true if the position is before or at the start of the text for this line
func (e *Editor) AtOrBeforeStartOfTextLine() bool {
	return e.pos.sx <= e.FirstScreenPosition(e.DataY())
}

// GoTo will go to a given line index, counting from 0
// Returns true if the editor should be redrawn
// status is used for clearing status bar messages and can be nil
func (e *Editor) GoTo(dataY int, c *vt100.Canvas, status *StatusBar) bool {
	if dataY == e.DataY() {
		// Already at the correct line, but still trigger a redraw
		return true
	}
	reachedEnd := false
	// Out of bounds checking for y
	if dataY < 0 {
		dataY = 0
	} else if dataY >= e.Len() {
		dataY = e.Len() - 1
		reachedEnd = true
	}
	h := 25
	if c != nil {
		// Get the current terminal height
		h = int(c.Height())
	}

	// Is the place we want to go within the current scroll window?
	topY := e.pos.offset
	botY := e.pos.offset + h

	if dataY >= topY && dataY < botY {
		// No scrolling is needed, just move the screen y position
		e.pos.sy = dataY - e.pos.offset
	} else if dataY < h {
		// No scrolling is needed, just move the screen y position
		e.pos.offset = 0
		e.pos.sy = dataY - 1
	} else if reachedEnd {
		// To the end of the text
		e.pos.offset = e.Len() - h
		e.pos.sy = h - 1
	} else {
		prevY := e.pos.sy
		// Scrolling is needed
		e.pos.sy = 0
		e.pos.offset = dataY
		lessJumpY := prevY
		lessJumpOffset := dataY - prevY
		if (lessJumpY + lessJumpOffset) < e.Len() {
			e.pos.sy = lessJumpY
			e.pos.offset = lessJumpOffset
		}
	}

	// The Y scrolling is done, move the X position according to the contents of the line
	e.pos.SetX(e.FirstScreenPosition(e.DataY()))

	// Clear all status messages
	if status != nil {
		status.ClearAll(c)
	}

	// Trigger cursor redraw
	e.redrawCursor = true

	// Should also redraw the text
	return true
}

// GoToLineNumber will go to a given line number, but counting from 1, not from 0!
func (e *Editor) GoToLineNumber(lineNumber int, c *vt100.Canvas, status *StatusBar, center bool) bool {
	// e.GoTo will check for this
	//if lineNumber >= e.Len() {
	//	return false
	//}
	redraw := e.GoTo(lineNumber-1, c, status)
	if redraw && center {
		e.Center(c)
	}
	return redraw
}

// Up tried to move the cursor up, and also scroll
func (e *Editor) Up(c *vt100.Canvas, status *StatusBar) {
	e.GoTo(e.DataY()-1, c, status)
}

// Down tries to move the cursor down, and also scroll
// status is used for clearing status bar messages and can be nil
func (e *Editor) Down(c *vt100.Canvas, status *StatusBar) {
	e.GoTo(e.DataY()+1, c, status)
}

// LeadingWhitespace returns the leading whitespace for this line
func (e *Editor) LeadingWhitespace() string {
	return e.CurrentLine()[:e.FirstDataPosition(e.DataY())]
}

// LineNumber will return the current line number (data y index + 1)
func (e *Editor) LineNumber() int {
	return e.DataY() + 1
}

// ColumnNumber will return the current column number (data x index + 1)
func (e *Editor) ColumnNumber() int {
	x, _ := e.DataX()
	return x + 1
}

// StatusMessage returns a status message, intended for being displayed at the bottom
func (e *Editor) StatusMessage() string {
	return fmt.Sprintf("line %d col %d rune %U words %d", e.LineNumber(), e.ColumnNumber(), e.Rune(), e.WordCount())
}

// DrawLines will draw a screen full of lines on the given canvas
func (e *Editor) DrawLines(c *vt100.Canvas, respectOffset, redraw bool) {
	h := int(c.Height())
	if respectOffset {
		e.WriteLines(c, e.pos.Offset(), h+e.pos.Offset(), 0, 0)
	} else {
		e.WriteLines(c, 0, h, 0, 0)
	}
	if redraw {
		c.Redraw()
	} else {
		c.Draw()
	}
}

// FullResetRedraw will completely reset and redraw everything, including creating a brand new Canvas struct
func (e *Editor) FullResetRedraw(c *vt100.Canvas, status *StatusBar) *vt100.Canvas {
	savePos := e.pos
	status.ClearAll(c)
	vt100.Close()
	vt100.Reset()
	vt100.Clear()
	vt100.Init()
	newC := vt100.NewCanvas()
	newC.ShowCursor()
	if int(newC.Width()) < e.wordWrapAt {
		e.wordWrapAt = int(newC.Width())
	}
	e.pos = savePos
	e.redraw = true
	e.redrawCursor = true
	return newC
}

// GoToPosition can go to the given position struct and use it as the new position
func (e *Editor) GoToPosition(c *vt100.Canvas, status *StatusBar, pos Position) {
	e.pos = pos
	e.redraw = e.GoTo(e.DataY(), c, status)
	e.redrawCursor = true
}

// GoToStartOfTextLine will go to the start of the non-whitespace text, for this line
func (e *Editor) GoToStartOfTextLine() {
	e.pos.SetX(e.FirstScreenPosition(e.DataY()))
}

// GoToNextParagraph will jump to the next line that has a blank line above it, if possible
// Returns true if the editor should be redrawn
func (e *Editor) GoToNextParagraph(c *vt100.Canvas, status *StatusBar) bool {
	lastFoundBlankLine := -1
	for i := e.DataY() + 1; i < e.Len(); i++ {
		// Check if this is a blank line
		if len(strings.TrimSpace(e.Line(i))) == 0 {
			lastFoundBlankLine = i
		} else {
			// This is a non-blank line, check if the line above is blank (or before the first line)
			if lastFoundBlankLine == (i - 1) {
				// Yes, this is the line we wish to jump to
				return e.GoTo(i, c, status)
			}
		}
	}
	return false
}

// GoToPrevParagraph will jump to the previous line that has a blank line below it, if possible
// Returns true if the editor should be redrawn
func (e *Editor) GoToPrevParagraph(c *vt100.Canvas, status *StatusBar) bool {
	lastFoundBlankLine := e.Len()
	for i := e.DataY() - 1; i >= 0; i-- {
		// Check if this is a blank line
		if len(strings.TrimSpace(e.Line(i))) == 0 {
			lastFoundBlankLine = i
		} else {
			// This is a non-blank line, check if the line below is blank (or after the last line)
			if lastFoundBlankLine == (i + 1) {
				// Yes, this is the line we wish to jump to
				return e.GoTo(i, c, status)
			}
		}
	}
	return false
}

// Center will scroll the contents so that the line with the cursor ends up in the center of the screen
func (e *Editor) Center(c *vt100.Canvas) {
	// Find the terminal height
	h := int(c.Height())

	// General information about how the positions and offsets relate:
	//
	// offset + screen y = data y
	//
	// offset = e.pos.offset
	// screen y = e.pos.sy
	// data y = e.DataY()
	//
	// offset = data y - screen y

	// Plan:
	// 1. offset = data y - (h / 2)
	// 2. screen y = data y - offset

	// Find the center line
	centerY := h / 2
	if e.DataY() < centerY {
		// Not enough room to adjust
		return
	}

	// Find the new offset and y position
	newOffset := e.DataY() - centerY
	newScreenY := e.DataY() - newOffset

	// Assign the new values to the editor
	e.pos.offset = newOffset
	e.pos.sy = newScreenY
}

// ToggleComment will toggle single-line comments on or off ("// ")
// There may be an indent before the comment
func (e *Editor) ToggleComment() {
	var (
		changed      bool
		newContents  string
		contents     = e.CurrentLine()
		trimContents = strings.TrimSpace(contents)
	)
	if strings.HasPrefix(trimContents, "// ") {
		// toggle off comment
		newContents = strings.Replace(contents, "// ", "", 1)
		changed = true
	} else if strings.HasPrefix(trimContents, "//") {
		// toggle off comment
		newContents = strings.Replace(contents, "//", "", 1)
		changed = true
	} else {
		// toggle on comment if there is no leading "//" (check without a trailing space this time)
		if !strings.HasPrefix(strings.TrimSpace(contents), "//") {
			newContents = e.LeadingWhitespace() + "// " + strings.TrimSpace(contents)
			changed = true
		}
	}
	if changed {
		e.SetLine(e.DataY(), newContents)
		if e.AfterEndOfLine() {
			e.End()
		}
	}
}

// CommentOn will insert "// " in front of the line
func (e *Editor) CommentOn() {
	newContents := "// " + e.CurrentLine()
	e.SetLine(e.DataY(), newContents)
	if e.AfterEndOfLine() {
		e.End()
	}
}

// CommentOff will remove "//" from the front of the line
func (e *Editor) CommentOff() {
	var (
		changed      bool
		newContents  string
		contents     = e.CurrentLine()
		trimContents = strings.TrimSpace(contents)
	)
	if strings.HasPrefix(trimContents, "// ") {
		// toggle off comment
		newContents = strings.Replace(contents, "// ", "", 1)
		changed = true
	} else if strings.HasPrefix(trimContents, "//") {
		// toggle off comment
		newContents = strings.Replace(contents, "//", "", 1)
		changed = true
	}
	if changed {
		e.SetLine(e.DataY(), newContents)
		if e.AfterEndOfLine() {
			e.End()
		}
	}
}

// CurrentLineCommented checks if the current trimmed line starts with "//"
func (e *Editor) CurrentLineCommented() bool {
	return strings.HasPrefix(strings.TrimSpace(e.CurrentLine()), "//")
}

// ForEachLineInBlock will move the cursor and run the given function for
// each line in the current block of text (until newline or end of document)
func (e *Editor) ForEachLineInBlock(c *vt100.Canvas, f func()) {
	downCounter := 0
	for !e.EmptyLine() && !e.AtOrAfterEndOfDocument() {
		f()
		e.Down(c, nil)
		downCounter++
	}
	// Go up again
	for i := downCounter; i > 0; i-- {
		e.Up(c, nil)
	}
}

// ToggleCommentBlock will toggle comments until a blank line or the end of the document is reached
// The amount of existing commented lines is considered before deciding to comment the block in or out
func (e *Editor) ToggleCommentBlock(c *vt100.Canvas) {
	// If most of the lines in the block are comments, comment it out
	// If most of the lines in the block are not comments, comment it in

	downCounter := 0
	commentCounter := 0

	// Count the commented lines in this block while going down
	for !e.EmptyLine() && !e.AtOrAfterEndOfDocument() {
		if e.CurrentLineCommented() {
			commentCounter++
		}
		e.Down(c, nil)
		downCounter++
	}
	// Go up again
	for i := downCounter; i > 0; i-- {
		e.Up(c, nil)
	}

	// Check if most lines are commented out
	mostLinesAreComments := commentCounter > (downCounter / 2)

	if mostLinesAreComments {
		e.ForEachLineInBlock(c, e.CommentOff)
	} else {
		e.ForEachLineInBlock(c, e.CommentOn)
	}
}

// NewLine inserts a new line below and moves down one step
func (e *Editor) NewLine(c *vt100.Canvas, status *StatusBar) {
	e.InsertLineBelow()
	e.Down(c, status)
}
