package installer

// A serial line carries no window size, so the installer asks the terminal: it moves the cursor to the far
// corner and asks where it ended up (a cursor position report, which nearly every terminal program answers).
// The reply arrives mixed in with typing, so it is picked out here and never reaches the wizard.

// sizeQuery sends the cursor to the bottom right and asks for its position, then puts it back.
const sizeQuery = "\x1b7\x1b[999;999H\x1b[6n\x1b8"

// cprFilter removes cursor position reports (ESC [ rows ; cols R) from what the terminal sends and
// reports the last one it saw. It works across reads, since a reply can arrive in pieces.
type cprFilter struct {
	pending []byte
}

// Feed returns the input with any reports taken out, and the size from the last complete report
// (rows, cols; zero when there was none).
func (f *cprFilter) Feed(in []byte) (out []byte, rows, cols int) {
	data := append(f.pending, in...)
	f.pending = nil
	for i := 0; i < len(data); {
		if data[i] != 0x1b {
			out = append(out, data[i])
			i++
			continue
		}
		// Possibly the start of a report: ESC [ digits ; digits R
		j := i + 1
		if j >= len(data) {
			f.pending = append(f.pending, data[i:]...)
			return
		}
		if data[j] != '[' {
			out = append(out, data[i])
			i++
			continue
		}
		j++
		r, c, n := 0, 0, 0
		for j < len(data) && data[j] >= '0' && data[j] <= '9' {
			r, n, j = r*10+int(data[j]-'0'), n+1, j+1
		}
		if j >= len(data) {
			f.pending = append(f.pending, data[i:]...)
			return
		}
		if n == 0 || data[j] != ';' {
			out = append(out, data[i]) // an ordinary key such as an arrow
			i++
			continue
		}
		j++
		m := 0
		for j < len(data) && data[j] >= '0' && data[j] <= '9' {
			c, m, j = c*10+int(data[j]-'0'), m+1, j+1
		}
		if j >= len(data) {
			f.pending = append(f.pending, data[i:]...)
			return
		}
		if m == 0 || data[j] != 'R' {
			out = append(out, data[i])
			i++
			continue
		}
		rows, cols = r, c
		i = j + 1
	}
	return
}

// sane says whether a reported size can be believed.
func sane(rows, cols int) bool { return rows >= 10 && rows <= 200 && cols >= 40 && cols <= 400 }
