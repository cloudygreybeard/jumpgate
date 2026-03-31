Host {{.Context}}-gate{{if .IsDefault}} gate{{end}}
  HostName {{.Hostname}}
{{- if .User}}
  User {{.User}}
{{- end}}
{{- if gt .Port 0}}
  Port {{.Port}}
{{- end}}
  GSSAPIAuthentication yes
  GSSAPIDelegateCredentials no
  ServerAliveInterval 30
