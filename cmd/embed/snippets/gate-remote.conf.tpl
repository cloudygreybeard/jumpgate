Host {{.Context}}-gate{{if .IsDefault}} gate{{end}}
  HostName {{.Hostname}}
{{- if .User}}
  User {{.User}}
{{- end}}
{{- if gt .Port 0}}
  Port {{.Port}}
{{- end}}
{{- if eq .AuthType "kerberos"}}
  GSSAPIAuthentication yes
  GSSAPIDelegateCredentials no
{{- else}}
  GSSAPIAuthentication no
  PreferredAuthentications publickey
{{- if .GateKey}}
  IdentityFile {{.GateKey}}
{{- end}}
{{- end}}
  MACs hmac-sha2-256,hmac-sha2-512
  ServerAliveInterval 30
