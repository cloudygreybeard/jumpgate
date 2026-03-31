Host {{.Context}}{{if .IsDefault}} remote{{end}}
  HostName localhost
{{- if .RemoteUser}}
  User {{.RemoteUser}}
{{- end}}
{{- if gt .RelayPort 0}}
  Port {{.RelayPort}}
{{- end}}
{{- if .RemoteKey}}
  IdentityFile {{.RemoteKey}}
{{- end}}
  AddKeysToAgent yes
  UseKeychain yes
  ProxyJump {{.Context}}-gate
