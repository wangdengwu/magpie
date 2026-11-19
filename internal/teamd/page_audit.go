package teamd

import "html/template"

// The audit page's view. It draws only what teamd recorded: this team's own
// lines, and nothing of another's.

func auditPage(v AuditView) string {
	return render(template.Must(template.New("audit").Funcs(pageFuncs(v.Trans)).Parse(auditHTML)), v)
}
