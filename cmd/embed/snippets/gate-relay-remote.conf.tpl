Host {{.Context}}-relay{{if .IsDefault}} relay{{end}}
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
{{- if gt .RelayPort 0}}
  RemoteForward {{.RelayPort}} localhost:22
{{- end}}
  ExitOnForwardFailure yes
  ControlMaster auto
  ControlPath {{.SocketDir}}/{{.Context}}-relay.sock
  ControlPersist yes
