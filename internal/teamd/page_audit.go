package teamd

import "html/template"

// The audit page's view. It draws only what teamd recorded: this team's own
// lines, and nothing of another's.

func auditPage(v AuditView) string {
	return render(template.Must(template.New("audit").Funcs(auditFuncs).Parse(auditHTML)), v)
}

// auditFuncs are what the page's template uses besides its values.
var auditFuncs = template.FuncMap{
	"ago":    ago,
	"plural": plural,
}
