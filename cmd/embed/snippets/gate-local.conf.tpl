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
  PreferredAuthentications gssapi-with-mic,keyboard-interactive
{{- else}}
  GSSAPIAuthentication no
  PreferredAuthentications publickey
{{- if .GateKey}}
  IdentityFile {{.GateKey}}
{{- end}}
{{- end}}
  ServerAliveInterval 30
  ControlMaster auto
  ControlPath {{.SocketDir}}/{{.Context}}-gate.sock
  ControlPersist 4h
