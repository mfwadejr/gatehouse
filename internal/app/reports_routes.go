package app

import (
	"archive/zip"
	"bytes"
	"encoding/csv"
	"encoding/json"
	"encoding/xml"
	"fmt"
	"net/http"
	"os"
	"regexp"
	"strconv"
	"strings"
	"time"

	"nopewarden/internal/api"
)

type savedReport struct {
	ID      int64     `json:"id"`
	Name    string    `json:"name"`
	Spec    json.RawMessage `json:"spec"`
	Created int64     `json:"created_at"`
	By      string    `json:"created_by"`
}

func (a *App) savedReports() []savedReport {
	out := []savedReport{}
	rows, err := a.Config.Query(`SELECT id, name, spec, created_at, created_by FROM saved_reports ORDER BY name COLLATE NOCASE`)
	if err != nil {
		return out
	}
	defer rows.Close()
	for rows.Next() {
		var s savedReport
		var spec string
		if rows.Scan(&s.ID, &s.Name, &spec, &s.Created, &s.By) == nil && json.Valid([]byte(spec)) {
			s.Spec = json.RawMessage(spec)
			out = append(out, s)
		}
	}
	return out
}

func (a *App) boxName() string {
	h, _ := os.Hostname()
	return h
}

func reportFileName(r *reportResp, ext string) string {
	s := strings.Trim(regexp.MustCompile(`[^a-z0-9]+`).ReplaceAllString(strings.ToLower(r.Title), "-"), "-")
	if s == "" {
		s = "report"
	}
	return fmt.Sprintf("nopewarden-%s-%s.%s", s, time.Unix(r.Made, 0).Format("2006-01-02"), ext)
}

func reportCSV(r *reportResp) []byte {
	var b bytes.Buffer
	w := csv.NewWriter(&b)
	w.Write(r.Columns)
	for _, row := range r.Rows {
		w.Write(row)
	}
	w.Flush()
	return b.Bytes()
}

func xlsxCol(i int) string {
	s := ""
	for i++; i > 0; i = (i - 1) / 26 {
		s = string(rune('A'+(i-1)%26)) + s
	}
	return s
}

func xlsxCell(ref, v string) string {
	var esc bytes.Buffer
	xml.EscapeText(&esc, []byte(v))
	if _, err := strconv.ParseFloat(v, 64); err == nil && v != "" && !strings.HasPrefix(v, "0") || v == "0" {
		return `<c r="` + ref + `"><v>` + esc.String() + `</v></c>`
	}
	return `<c r="` + ref + `" t="inlineStr"><is><t xml:space="preserve">` + esc.String() + `</t></is></c>`
}

func xlsxSheet(rows [][]string) string {
	var b strings.Builder
	b.WriteString(`<?xml version="1.0" encoding="UTF-8" standalone="yes"?><worksheet xmlns="http://schemas.openxmlformats.org/spreadsheetml/2006/main"><sheetData>`)
	for ri, row := range rows {
		fmt.Fprintf(&b, `<row r="%d">`, ri+1)
		for ci, v := range row {
			b.WriteString(xlsxCell(xlsxCol(ci)+strconv.Itoa(ri+1), v))
		}
		b.WriteString(`</row>`)
	}
	b.WriteString(`</sheetData></worksheet>`)
	return b.String()
}

// reportXLSX makes a workbook with the summary on one sheet and the rows on another.
func reportXLSX(r *reportResp, box string) []byte {
	sum := [][]string{{r.Title}, {"Period", time.Unix(r.From, 0).Format("2006-01-02 15:04") + " to " + time.Unix(r.To, 0).Format("2006-01-02 15:04")},
		{"Grouped by", r.Group}, {"Filters", strings.Join(r.Filters, "; ")},
		{"Made", time.Unix(r.Made, 0).Format("2006-01-02 15:04")}, {"Box", box}, {}}
	for _, s := range r.Summary {
		sum = append(sum, []string{s.Label, s.Value})
	}
	for _, n := range r.Notes {
		sum = append(sum, []string{"Note", n})
	}
	rows := append([][]string{r.Columns}, r.Rows...)
	var out bytes.Buffer
	z := zip.NewWriter(&out)
	add := func(name, body string) {
		f, _ := z.Create(name)
		f.Write([]byte(body))
	}
	const hdr = `<?xml version="1.0" encoding="UTF-8" standalone="yes"?>`
	add("[Content_Types].xml", hdr+`<Types xmlns="http://schemas.openxmlformats.org/package/2006/content-types"><Default Extension="rels" ContentType="application/vnd.openxmlformats-package.relationships+xml"/><Default Extension="xml" ContentType="application/xml"/><Override PartName="/xl/workbook.xml" ContentType="application/vnd.openxmlformats-officedocument.spreadsheetml.sheet.main+xml"/><Override PartName="/xl/worksheets/sheet1.xml" ContentType="application/vnd.openxmlformats-officedocument.spreadsheetml.worksheet+xml"/><Override PartName="/xl/worksheets/sheet2.xml" ContentType="application/vnd.openxmlformats-officedocument.spreadsheetml.worksheet+xml"/></Types>`)
	add("_rels/.rels", hdr+`<Relationships xmlns="http://schemas.openxmlformats.org/package/2006/relationships"><Relationship Id="rId1" Type="http://schemas.openxmlformats.org/officeDocument/2006/relationships/officeDocument" Target="xl/workbook.xml"/></Relationships>`)
	add("xl/workbook.xml", hdr+`<workbook xmlns="http://schemas.openxmlformats.org/spreadsheetml/2006/main" xmlns:r="http://schemas.openxmlformats.org/officeDocument/2006/relationships"><sheets><sheet name="Summary" sheetId="1" r:id="rId1"/><sheet name="Rows" sheetId="2" r:id="rId2"/></sheets></workbook>`)
	add("xl/_rels/workbook.xml.rels", hdr+`<Relationships xmlns="http://schemas.openxmlformats.org/package/2006/relationships"><Relationship Id="rId1" Type="http://schemas.openxmlformats.org/officeDocument/2006/relationships/worksheet" Target="worksheets/sheet1.xml"/><Relationship Id="rId2" Type="http://schemas.openxmlformats.org/officeDocument/2006/relationships/worksheet" Target="worksheets/sheet2.xml"/></Relationships>`)
	add("xl/worksheets/sheet1.xml", xlsxSheet(sum))
	add("xl/worksheets/sheet2.xml", xlsxSheet(rows))
	z.Close()
	return out.Bytes()
}

func (a *App) registerReportRoutes() {
	a.API.Handle("POST /api/reports/run", func(w http.ResponseWriter, r *http.Request, c *api.Ctx) {
		var req reportReq
		if !api.Decode(w, r, &req) {
			return
		}
		res, err := a.runReport(req, time.Now())
		if err != nil {
			api.Error(w, 400, err.Error())
			return
		}
		api.JSON(w, 200, res)
	})
	a.API.Handle("POST /api/reports/export", func(w http.ResponseWriter, r *http.Request, c *api.Ctx) {
		var req reportReq
		if !api.Decode(w, r, &req) {
			return
		}
		res, err := a.runReport(req, time.Now())
		if err != nil {
			api.Error(w, 400, err.Error())
			return
		}
		var body []byte
		var ctype, ext string
		switch f := r.URL.Query().Get("format"); f {
		case "csv":
			body, ctype, ext = reportCSV(res), "text/csv; charset=utf-8", "csv"
		case "json":
			body, _ = json.MarshalIndent(res, "", "  ")
			ctype, ext = "application/json", "json"
		case "xlsx":
			body, ctype, ext = reportXLSX(res, a.boxName()), "application/vnd.openxmlformats-officedocument.spreadsheetml.sheet", "xlsx"
		case "pdf":
			body, ctype, ext = reportPDF(res, a.boxName()), "application/pdf", "pdf"
		default:
			api.Error(w, 400, "unknown format")
			return
		}
		w.Header().Set("Content-Type", ctype)
		w.Header().Set("Content-Disposition", `attachment; filename="`+reportFileName(res, ext)+`"`)
		w.Header().Set("Cache-Control", "no-store")
		w.Write(body)
	})
	a.API.Handle("GET /api/reports/saved", func(w http.ResponseWriter, r *http.Request, c *api.Ctx) {
		api.JSON(w, 200, a.savedReports())
	})
	a.API.HandleAdmin("POST /api/reports/saved", func(w http.ResponseWriter, r *http.Request, c *api.Ctx) {
		var in struct {
			Name string    `json:"name"`
			Spec json.RawMessage `json:"spec"`
		}
		if !api.Decode(w, r, &in) {
			return
		}
		in.Name = strings.TrimSpace(in.Name)
		if in.Name == "" || len(in.Name) > 80 {
			api.Error(w, 400, "Give the report a name (up to 80 letters).")
			return
		}
		var probe map[string]any
		if len(in.Spec) > 8192 || json.Unmarshal(in.Spec, &probe) != nil {
			api.Error(w, 400, "That report could not be saved.")
			return
		}
		spec := in.Spec
		if _, err := a.Config.Exec(`INSERT INTO saved_reports(name, spec, created_at, created_by) VALUES(?,?,?,?)
			ON CONFLICT(name) DO UPDATE SET spec=excluded.spec`, in.Name, string(spec), time.Now().Unix(), c.User.Username); err != nil {
			api.Error(w, 500, "Could not save the report.")
			return
		}
		a.event(c.User.Username, "info", "Report saved", in.Name)
		api.JSON(w, 200, a.savedReports())
	})
	a.API.HandleAdmin("DELETE /api/reports/saved/{id}", func(w http.ResponseWriter, r *http.Request, c *api.Ctx) {
		id, _ := strconv.ParseInt(r.PathValue("id"), 10, 64)
		a.Config.Exec(`DELETE FROM saved_reports WHERE id=?`, id)
		api.JSON(w, 200, a.savedReports())
	})
}
