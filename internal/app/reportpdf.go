package app

import (
	"bytes"
	"fmt"
	"math"
	"strings"
	"time"
)

// A small PDF writer for reports, built into the program so a report looks the same whatever device
// asks for it. It uses the PDF standard fonts (Helvetica), which every PDF reader has, so nothing is
// downloaded and no font file is needed. They cover Western letters; other characters print as "?".

const (
	pdfW, pdfH   = 595.0, 842.0
	pdfMargin    = 40.0
	pdfBodyWidth = pdfW - 2*pdfMargin
)

// The shield from brand/logo.svg (a test keeps this copy in step with that file).
const logoShieldPath = "M16 2.5 5 6.5v8.2c0 6.6 4.5 12.2 11 14.8 6.5-2.6 11-8.2 11-14.8V6.5L16 2.5Z"

var helv = [95]int{278, 278, 355, 556, 556, 889, 667, 191, 333, 333, 389, 584, 278, 333, 278, 278,
	556, 556, 556, 556, 556, 556, 556, 556, 556, 556, 278, 278, 584, 584, 584, 556, 1015,
	667, 667, 722, 722, 667, 611, 778, 722, 278, 500, 667, 556, 833, 722, 778, 667, 778, 722, 667, 611, 722, 667, 944, 667, 667, 611,
	278, 278, 278, 469, 556, 333,
	556, 556, 500, 556, 556, 278, 556, 556, 222, 222, 500, 222, 833, 556, 556, 556, 556, 333, 500, 278, 556, 500, 722, 500, 500, 500,
	334, 260, 334, 584}

type pdfDoc struct {
	pages  []*bytes.Buffer
	cur    *bytes.Buffer
	y      float64
	header func(d *pdfDoc) // drawn at the top of every page after the first
}

func pdfClean(s string) string {
	var b strings.Builder
	for _, r := range s {
		switch {
		case r >= 32 && r < 127:
			b.WriteRune(r)
		case r == '–' || r == '—' || r == '−':
			b.WriteByte('-')
		case r == '’' || r == '‘':
			b.WriteByte('\'')
		case r == '“' || r == '”':
			b.WriteByte('"')
		case r == '…':
			b.WriteString("...")
		case r == '°':
			b.WriteString("\\260")
		case r == '\n' || r == '\t':
			b.WriteByte(' ')
		default:
			b.WriteByte('?')
		}
	}
	return b.String()
}

func pdfEsc(s string) string {
	s = pdfClean(s)
	s = strings.ReplaceAll(s, "\\260", "\x00") // keep the degree sign through escaping
	s = strings.NewReplacer("\\", "\\\\", "(", "\\(", ")", "\\)").Replace(s)
	return strings.ReplaceAll(s, "\x00", "\\260")
}

func pdfWidth(s string, size float64, bold bool) float64 {
	w := 0
	for _, r := range pdfClean(s) {
		if r >= 32 && r < 127 {
			w += helv[r-32]
		} else {
			w += 556
		}
	}
	f := 1.0
	if bold {
		f = 1.06
	}
	return float64(w) * size / 1000 * f
}

func (d *pdfDoc) newPage() {
	d.cur = &bytes.Buffer{}
	d.pages = append(d.pages, d.cur)
	d.y = pdfH - pdfMargin
	if len(d.pages) > 1 && d.header != nil {
		d.header(d)
	}
}

func (d *pdfDoc) need(h float64) {
	if d.cur == nil || d.y-h < pdfMargin+24 {
		d.newPage()
	}
}

func (d *pdfDoc) color(c [3]float64, stroke bool) {
	op := "rg"
	if stroke {
		op = "RG"
	}
	fmt.Fprintf(d.cur, "%.3f %.3f %.3f %s\n", c[0], c[1], c[2], op)
}

func (d *pdfDoc) text(x, y, size float64, bold bool, col [3]float64, s string) {
	f := "F1"
	if bold {
		f = "F2"
	}
	d.color(col, false)
	fmt.Fprintf(d.cur, "BT /%s %.1f Tf %.1f %.1f Td (%s) Tj ET\n", f, size, x, y, pdfEsc(s))
}

func (d *pdfDoc) rect(x, y, w, h float64, col [3]float64) {
	d.color(col, false)
	fmt.Fprintf(d.cur, "%.1f %.1f %.1f %.1f re f\n", x, y, w, h)
}

func (d *pdfDoc) line(x1, y1, x2, y2, width float64, col [3]float64) {
	d.color(col, true)
	fmt.Fprintf(d.cur, "%.2f w %.1f %.1f m %.1f %.1f l S\n", width, x1, y1, x2, y2)
}

// wrap breaks text into lines no wider than width.
func pdfWrap(s string, width, size float64, bold bool) []string {
	var lines []string
	cur := ""
	for _, w := range strings.Fields(s) {
		try := w
		if cur != "" {
			try = cur + " " + w
		}
		if pdfWidth(try, size, bold) <= width || cur == "" {
			cur = try
			continue
		}
		lines = append(lines, cur)
		cur = w
	}
	if cur != "" {
		lines = append(lines, cur)
	}
	return lines
}

func (d *pdfDoc) para(size float64, bold bool, col [3]float64, s string) {
	for _, ln := range pdfWrap(s, pdfBodyWidth, size, bold) {
		d.need(size + 4)
		d.y -= size + 2
		d.text(pdfMargin, d.y, size, bold, col, ln)
	}
	d.y -= 2
}

func pdfFit(s string, width, size float64, bold bool) string {
	if pdfWidth(s, size, bold) <= width {
		return s
	}
	for len(s) > 1 && pdfWidth(s+"...", size, bold) > width {
		s = s[:len(s)-1]
	}
	return s + "..."
}

// shield draws the NopeWarden shield at (x, y) with the given size in points, upper left corner.
func (d *pdfDoc) shield(x, y, size float64) {
	sc := size / 32
	px := func(v float64) float64 { return x + v*sc }
	py := func(v float64) float64 { return y - v*sc }
	d.color([3]float64{0.42, 0.55, 1}, false)
	c := d.cur
	fmt.Fprintf(c, "%.2f %.2f m %.2f %.2f l %.2f %.2f l ", px(16), py(2.5), px(5), py(6.5), px(5), py(14.7))
	fmt.Fprintf(c, "%.2f %.2f %.2f %.2f %.2f %.2f c ", px(5), py(21.3), px(9.5), py(26.9), px(16), py(29.5))
	fmt.Fprintf(c, "%.2f %.2f %.2f %.2f %.2f %.2f c ", px(22.5), py(26.9), px(27), py(21.3), px(27), py(14.7))
	fmt.Fprintf(c, "%.2f %.2f l h f\n", px(27), py(6.5))
	d.line(px(11), py(15.5), px(21), py(15.5), 2.6*sc, [3]float64{1, 1, 1})
}

var pdfSeriesColors = [][3]float64{{0.24, 0.45, 0.85}, {0.85, 0.42, 0.22}, {0.2, 0.62, 0.45}, {0.62, 0.35, 0.8}, {0.8, 0.65, 0.15}, {0.4, 0.4, 0.45}}

func (d *pdfDoc) chart(c reportChart, from, to int64) {
	const h = 130.0
	d.need(h + 40)
	d.y -= 6
	d.text(pdfMargin, d.y-10, 10, true, [3]float64{0.1, 0.1, 0.12}, c.Title+unitSuffix(c.Unit))
	d.y -= 18
	top := d.y
	bottom := d.y - h
	grey := [3]float64{0.8, 0.8, 0.82}
	if len(c.Bars) > 0 {
		max := 0.0
		for _, b := range c.Bars {
			max = math.Max(max, b.Value)
		}
		rowH := h / float64(len(c.Bars))
		if rowH > 18 {
			rowH = 18
		}
		for i, b := range c.Bars {
			yy := top - float64(i+1)*rowH
			d.text(pdfMargin, yy+4, 8, false, [3]float64{0.2, 0.2, 0.22}, pdfFit(b.Label, 150, 8, false))
			w := 0.0
			if max > 0 {
				w = b.Value / max * (pdfBodyWidth - 200)
			}
			d.rect(pdfMargin+160, yy+2, math.Max(w, 1), rowH-6, pdfSeriesColors[0])
			d.text(pdfMargin+165+w, yy+4, 8, false, [3]float64{0.3, 0.3, 0.32}, fnum(b.Value, 0))
		}
		d.y = top - float64(len(c.Bars))*rowH - 8
		return
	}
	left := pdfMargin + 34
	width := pdfBodyWidth - 34
	lo, hi := math.Inf(1), math.Inf(-1)
	for _, s := range c.Series {
		for _, p := range s.Pts {
			lo, hi = math.Min(lo, p[1]), math.Max(hi, p[1])
		}
	}
	if math.IsInf(lo, 1) {
		d.text(left, top-h/2, 9, false, [3]float64{0.45, 0.45, 0.5}, "No data in this period.")
		d.y = bottom - 8
		return
	}
	if lo > 0 {
		lo = 0
	}
	if hi <= lo {
		hi = lo + 1
	}
	for i := 0; i <= 4; i++ {
		yy := bottom + h*float64(i)/4
		d.line(left, yy, left+width, yy, 0.4, grey)
		d.text(pdfMargin, yy-3, 7, false, [3]float64{0.4, 0.4, 0.45}, fnum(lo+(hi-lo)*float64(i)/4, 1))
	}
	span := float64(to - from)
	for si, s := range c.Series {
		col := pdfSeriesColors[si%len(pdfSeriesColors)]
		if len(s.Pts) == 1 {
			p := s.Pts[0]
			d.rect(left+(p[0]-float64(from))/span*width-2, bottom+(p[1]-lo)/(hi-lo)*h-2, 4, 4, col)
			continue
		}
		d.color(col, true)
		fmt.Fprintf(d.cur, "1 w ")
		for i, p := range s.Pts {
			x := left + (p[0]-float64(from))/span*width
			y := bottom + (p[1]-lo)/(hi-lo)*h
			if i == 0 {
				fmt.Fprintf(d.cur, "%.1f %.1f m ", x, y)
			} else {
				fmt.Fprintf(d.cur, "%.1f %.1f l ", x, y)
			}
		}
		fmt.Fprintf(d.cur, "S\n")
	}
	d.text(left, bottom-10, 7, false, [3]float64{0.4, 0.4, 0.45}, time.Unix(from, 0).Format("Jan 2, 2006"))
	end := time.Unix(to, 0).Format("Jan 2, 2006")
	d.text(left+width-pdfWidth(end, 7, false), bottom-10, 7, false, [3]float64{0.4, 0.4, 0.45}, end)
	// The legend: a coloured square and a name for each line.
	lx := left
	for si, s := range c.Series {
		col := pdfSeriesColors[si%len(pdfSeriesColors)]
		name := pdfFit(s.Name, 120, 8, false)
		d.rect(lx, bottom-24, 7, 7, col)
		d.text(lx+10, bottom-23, 8, false, [3]float64{0.2, 0.2, 0.22}, name)
		lx += 20 + pdfWidth(name, 8, false)
		if lx > left+width-80 {
			break
		}
	}
	d.y = bottom - 32
}

func unitSuffix(u string) string {
	if u == "" {
		return ""
	}
	return " (" + u + ")"
}

func (d *pdfDoc) table(cols []string, rows [][]string) {
	if len(cols) == 0 {
		return
	}
	const size, rowH = 8.0, 13.0
	// Column widths follow the widest text, within limits, then scale to fit the page.
	w := make([]float64, len(cols))
	for i, c := range cols {
		w[i] = pdfWidth(c, size, true) + 8
	}
	for _, r := range rows[:minInt(len(rows), 200)] {
		for i := range cols {
			if i < len(r) {
				w[i] = math.Max(w[i], math.Min(pdfWidth(r[i], size, false)+8, 190))
			}
		}
	}
	tot := 0.0
	for _, x := range w {
		tot += x
	}
	for i := range w {
		w[i] *= pdfBodyWidth / tot
	}
	head := func() {
		d.need(rowH * 2)
		d.rect(pdfMargin, d.y-rowH, pdfBodyWidth, rowH, [3]float64{0.93, 0.93, 0.95})
		x := pdfMargin
		for i, c := range cols {
			d.text(x+3, d.y-rowH+4, size, true, [3]float64{0.1, 0.1, 0.12}, pdfFit(c, w[i]-6, size, true))
			x += w[i]
		}
		d.y -= rowH
	}
	head()
	for ri, r := range rows {
		if d.y-rowH < pdfMargin+24 {
			d.newPage()
			head()
		}
		if ri%2 == 1 {
			d.rect(pdfMargin, d.y-rowH, pdfBodyWidth, rowH, [3]float64{0.98, 0.98, 0.99})
		}
		x := pdfMargin
		for i := range cols {
			v := ""
			if i < len(r) {
				v = r[i]
			}
			d.text(x+3, d.y-rowH+4, size, false, [3]float64{0.15, 0.15, 0.18}, pdfFit(v, w[i]-6, size, false))
			x += w[i]
		}
		d.y -= rowH
	}
}

func minInt(a, b int) int {
	if a < b {
		return a
	}
	return b
}

// reportPDF lays a report out: the shield and name, the title, period, filters, when and where it was
// made, the summary, the charts, then the table, with page numbers.
func reportPDF(r *reportResp, box string) []byte {
	ink := [3]float64{0.1, 0.1, 0.12}
	mute := [3]float64{0.4, 0.4, 0.45}
	d := &pdfDoc{}
	d.header = func(d *pdfDoc) {
		d.text(pdfMargin, d.y-8, 8, false, mute, "NopeWarden: "+r.Title)
		d.y -= 18
	}
	d.newPage()
	d.shield(pdfMargin, d.y, 34)
	d.text(pdfMargin+44, d.y-18, 20, true, ink, "NopeWarden")
	d.text(pdfMargin+44, d.y-32, 9, false, mute, "Hard on threats. Easy on humans.")
	d.y -= 52
	d.line(pdfMargin, d.y, pdfW-pdfMargin, d.y, 0.8, [3]float64{0.8, 0.8, 0.84})
	d.y -= 10
	d.need(30)
	d.y -= 18
	d.text(pdfMargin, d.y, 16, true, ink, r.Title)
	d.y -= 4
	period := time.Unix(r.From, 0).In(time.Local).Format("Jan 2, 2006 15:04") + " to " + time.Unix(r.To, 0).In(time.Local).Format("Jan 2, 2006 15:04")
	d.para(9, false, mute, "Period: "+period+". Grouped by "+r.Group+".")
	if len(r.Filters) > 0 {
		d.para(9, false, mute, "Filters: "+strings.Join(r.Filters, "; ")+".")
	} else {
		d.para(9, false, mute, "Filters: none.")
	}
	made := "Made " + time.Unix(r.Made, 0).In(time.Local).Format("Jan 2, 2006 15:04")
	if box != "" {
		made += " on " + box
	}
	d.para(9, false, mute, made+".")
	if len(r.Summary) > 0 {
		d.y -= 6
		d.need(30)
		d.y -= 12
		d.text(pdfMargin, d.y, 11, true, ink, "Summary")
		d.y -= 2
		for _, s := range r.Summary {
			d.need(14)
			d.y -= 12
			d.text(pdfMargin, d.y, 9, false, mute, pdfFit(s.Label, 200, 9, false))
			d.text(pdfMargin+210, d.y, 9, true, ink, pdfFit(s.Value, pdfBodyWidth-210, 9, true))
		}
	}
	for _, c := range r.Charts {
		if len(c.Series) == 0 && len(c.Bars) == 0 {
			continue
		}
		d.chart(c, r.From, r.To)
		break // the first chart carries the report; the console shows the others
	}
	for _, n := range r.Notes {
		d.para(8, false, mute, n)
	}
	if len(r.Rows) > 0 {
		d.y -= 8
		d.need(40)
		d.y -= 12
		d.text(pdfMargin, d.y, 11, true, ink, "Details")
		d.y -= 4
		d.table(r.Columns, r.Rows)
	}
	return d.finish(r.Title, time.Unix(r.Made, 0).In(time.Local).Format("Jan 2, 2006 15:04"))
}

func (d *pdfDoc) finish(title, made string) []byte {
	n := len(d.pages)
	for i, p := range d.pages {
		d.cur = p
		foot := fmt.Sprintf("Page %d of %d", i+1, n)
		d.text(pdfW-pdfMargin-pdfWidth(foot, 8, false), 24, 8, false, [3]float64{0.45, 0.45, 0.5}, foot)
		d.text(pdfMargin, 24, 8, false, [3]float64{0.45, 0.45, 0.5}, "NopeWarden report, made "+made)
	}
	var out bytes.Buffer
	var offs []int
	obj := func(body string) {
		offs = append(offs, out.Len())
		fmt.Fprintf(&out, "%d 0 obj\n%s\nendobj\n", len(offs), body)
	}
	out.WriteString("%PDF-1.4\n")
	obj("<< /Type /Catalog /Pages 2 0 R >>")
	kids := make([]string, n)
	for i := range kids {
		kids[i] = fmt.Sprintf("%d 0 R", 6+i*2)
	}
	obj(fmt.Sprintf("<< /Type /Pages /Kids [%s] /Count %d >>", strings.Join(kids, " "), n))
	obj("<< /Type /Font /Subtype /Type1 /BaseFont /Helvetica /Encoding /WinAnsiEncoding >>")
	obj("<< /Type /Font /Subtype /Type1 /BaseFont /Helvetica-Bold /Encoding /WinAnsiEncoding >>")
	for _, p := range d.pages {
		cnt := len(offs) + 1
		obj(fmt.Sprintf("<< /Length %d >>\nstream\n%s\nendstream", p.Len(), p.String()))
		obj(fmt.Sprintf("<< /Type /Page /Parent 2 0 R /MediaBox [0 0 %.0f %.0f] /Resources << /Font << /F1 3 0 R /F2 4 0 R >> >> /Contents %d 0 R >>", pdfW, pdfH, cnt))
	}
	infoNum := len(offs) + 1
	obj(fmt.Sprintf("<< /Title (%s) /Producer (NopeWarden) >>", pdfEsc(title)))
	xref := out.Len()
	fmt.Fprintf(&out, "xref\n0 %d\n0000000000 65535 f \n", len(offs)+1)
	for _, o := range offs {
		fmt.Fprintf(&out, "%010d 00000 n \n", o)
	}
	fmt.Fprintf(&out, "trailer\n<< /Size %d /Root 1 0 R /Info %d 0 R >>\nstartxref\n%d\n%%%%EOF\n", len(offs)+1, infoNum, xref)
	return out.Bytes()
}
