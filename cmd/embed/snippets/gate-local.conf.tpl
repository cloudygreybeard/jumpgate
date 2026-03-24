Host {{.Context}}-gate{{if .IsDefault}} gate{{end}}
  HostName {{.Hostname}}
  User {{.User}}
  Port {{.Port}}
  GSSAPIAuthentication yes
  GSSAPIDelegateCredentials no
  PreferredAuthentications gssapi-with-mic,keyboard-interactive
  ServerAliveInterval 30
  ControlMaster auto
  ControlPath {{.SocketDir}}/{{.Context}}-gate.sock
  ControlPersist 4h
