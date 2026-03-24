Host {{.Context}}-gate{{if .IsDefault}} gate{{end}}
  HostName {{.Hostname}}
  User {{.User}}
  Port {{.Port}}
  GSSAPIAuthentication yes
  GSSAPIDelegateCredentials no
  ServerAliveInterval 30
