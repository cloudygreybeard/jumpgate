Host {{.Context}}{{if .IsDefault}} remote{{end}}
  HostName localhost
  User {{.RemoteUser}}
  Port {{.RelayPort}}
  IdentityFile {{.RemoteKey}}
  AddKeysToAgent yes
  UseKeychain yes
  ProxyJump {{.Context}}-gate
