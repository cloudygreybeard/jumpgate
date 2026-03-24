Host {{.Context}}-relay{{if .IsDefault}} relay{{end}}
  HostName {{.Hostname}}
  User {{.User}}
  Port {{.Port}}
  GSSAPIAuthentication yes
  GSSAPIDelegateCredentials no
  ServerAliveInterval 30
  RemoteForward {{.RelayPort}} localhost:22
  ExitOnForwardFailure yes
