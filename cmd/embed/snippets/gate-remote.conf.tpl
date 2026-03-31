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
  MACs hmac-sha2-256,hmac-sha2-512
  ServerAliveInterval 30
