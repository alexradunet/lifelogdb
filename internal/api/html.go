package api

import (
	"encoding/json"
	"fmt"
	"html/template"
	"io"
	"sort"
	"strings"
)

// The browser view is one generic template over any entity: properties, links, embedded links, and every
// action as a form. It knows no resource; what it shows is what the API offers.
var page = template.Must(template.New("page").Funcs(template.FuncMap{
	"value": renderValue,
	"rel":   func(r []string) string { return strings.Join(r, " ") },
	"post":  func(a Action) bool { return a.Method == "POST" },
}).Parse(`<!doctype html>
<html lang="en"><head><meta charset="utf-8"><meta name="viewport" content="width=device-width, initial-scale=1">
<title>{{.Title}} · Lifelog</title>
<style>
:root{--bg:#fff;--fg:#1d1d1f;--muted:#6e6e73;--line:#d2d2d7;--accent:#0a5cc2;--panel:#f5f5f7}
@media (prefers-color-scheme:dark){:root{--bg:#161618;--fg:#ececf0;--muted:#9a9aa2;--line:#3a3a3e;--accent:#6aa8ff;--panel:#212124}}
body{background:var(--bg);color:var(--fg);font:15px/1.5 system-ui,sans-serif;max-width:860px;margin:0 auto;padding:16px}
a{color:var(--accent);text-decoration:none}a:hover{text-decoration:underline}
nav{display:flex;flex-wrap:wrap;gap:4px 14px;font-size:14px;border-bottom:1px solid var(--line);padding-bottom:8px}
h1{font-size:22px;margin:16px 0 4px}.class{color:var(--muted);font-size:13px}
pre{white-space:pre-wrap;background:var(--panel);padding:10px;border-radius:6px;margin:0}
table{border-collapse:collapse;width:100%;font-size:14px}td,th{border-bottom:1px solid var(--line);padding:4px 6px;text-align:left;vertical-align:top}
th{color:var(--muted);font-weight:500;width:1%;white-space:nowrap}
section{margin:18px 0}h2{font-size:15px;color:var(--muted);margin:0 0 6px;font-weight:600}
ul{margin:0;padding-left:18px}.rel{color:var(--muted);font-size:12px}
details{border:1px solid var(--line);border-radius:6px;padding:6px 10px;margin:6px 0}summary{cursor:pointer;font-weight:600}
form label{display:block;margin:6px 0 2px;font-size:13px;color:var(--muted)}
input,textarea,select{width:100%;box-sizing:border-box;font:inherit;padding:6px;background:var(--bg);color:var(--fg);border:1px solid var(--line);border-radius:4px}
textarea{min-height:8em;font-family:ui-monospace,monospace}button{margin-top:8px;font:inherit;padding:6px 14px}
.result{background:var(--panel);border-left:3px solid var(--accent);padding:8px 12px}
.error{border-left-color:#d33}
</style></head><body>
<nav>{{range .Links}}<a href="{{.Href}}" rel="{{rel .Rel}}">{{or .Title .Href}}</a>{{end}}</nav>
<h1>{{.Title}}</h1><div class="class">{{rel .Class}}</div>
{{with .Result}}<section class="result"><h2>Result</h2>{{value .}}</section>{{end}}
{{with .Properties}}<section>{{value .}}</section>{{end}}
{{with .Entities}}<section><h2>Links</h2><ul>{{range .}}<li><a href="{{.Href}}">{{or .Title .Href}}</a> <span class="rel">{{rel .Rel}}</span></li>{{end}}</ul></section>{{end}}
{{with .Actions}}<section><h2>Actions</h2>{{range .}}
<details><summary>{{or .Title .Name}}</summary>{{with .Description}}<p class="rel">{{.}}</p>{{end}}
<form method="{{.Method}}" action="{{.Href}}">{{range .Fields}}
{{if eq .Type "hidden"}}<input type="hidden" name="{{.Name}}" value="{{.Value}}">
{{else}}<label>{{or .Title .Name}}{{if .Required}} *{{end}}</label>
{{if .Options}}<select name="{{.Name}}">{{$v := .Value}}{{range .Options}}<option{{if eq . (printf "%v" $v)}} selected{{end}}>{{.}}</option>{{end}}</select>
{{else if eq .Type "textarea"}}<textarea name="{{.Name}}">{{with .Value}}{{.}}{{end}}</textarea>
{{else}}<input type="{{.Type}}" name="{{.Name}}" value="{{with .Value}}{{.}}{{end}}"{{if .Required}} required{{end}} step="any">{{end}}
{{end}}{{end}}<button>{{or .Title .Name}}</button></form></details>{{end}}</section>{{end}}
</body></html>`))

func renderHTML(w io.Writer, e *Entity) {
	if err := page.Execute(w, e); err != nil {
		fmt.Fprintf(w, "<pre>%s</pre>", template.HTMLEscapeString(err.Error()))
	}
}

// renderValue shows any property value: objects as two-column tables, lists of objects as tables, long text as pre.
func renderValue(v any) template.HTML {
	b, _ := json.Marshal(v)
	var x any
	json.Unmarshal(b, &x)
	var sb strings.Builder
	writeValue(&sb, x)
	return template.HTML(sb.String())
}

func writeValue(sb *strings.Builder, x any) {
	esc := template.HTMLEscapeString
	switch x := x.(type) {
	case map[string]any:
		keys := make([]string, 0, len(x))
		for k := range x {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		sb.WriteString("<table>")
		for _, k := range keys {
			sb.WriteString("<tr><th>" + esc(k) + "</th><td>")
			writeValue(sb, x[k])
			sb.WriteString("</td></tr>")
		}
		sb.WriteString("</table>")
	case []any:
		if len(x) == 0 {
			sb.WriteString(`<span class="rel">none</span>`)
			return
		}
		if first, ok := x[0].(map[string]any); ok {
			var cols []string
			for k := range first {
				cols = append(cols, k)
			}
			sort.Strings(cols)
			sb.WriteString("<table><tr>")
			for _, c := range cols {
				sb.WriteString("<th>" + esc(c) + "</th>")
			}
			sb.WriteString("</tr>")
			for _, row := range x {
				m, _ := row.(map[string]any)
				sb.WriteString("<tr>")
				for _, c := range cols {
					sb.WriteString("<td>")
					writeValue(sb, m[c])
					sb.WriteString("</td>")
				}
				sb.WriteString("</tr>")
			}
			sb.WriteString("</table>")
			return
		}
		sb.WriteString("<ul>")
		for _, it := range x {
			sb.WriteString("<li>")
			writeValue(sb, it)
			sb.WriteString("</li>")
		}
		sb.WriteString("</ul>")
	case string:
		if strings.Contains(x, "\n") || len(x) > 80 {
			sb.WriteString("<pre>" + esc(x) + "</pre>")
		} else {
			sb.WriteString(esc(x))
		}
	case nil:
		sb.WriteString(`<span class="rel">—</span>`)
	default:
		sb.WriteString(esc(fmt.Sprint(x)))
	}
}
