module github.com/supabase-manager/proxy-agent

go 1.27.1

require (
	github.com/supabase-manager/shared v0.0.0
	github.com/supabase-manager/version v0.0.0
	google.golang.org/grpc v1.84.0
)

require (
	golang.org/x/net v0.59.0 // indirect
	golang.org/x/sys v0.48.0 // indirect
	golang.org/x/text v0.42.0 // indirect
	google.golang.org/genproto/googleapis/rpc v0.0.0-20260928230214-8a89bd6388cc // indirect
	google.golang.org/protobuf v1.36.12 // indirect
)

replace github.com/supabase-manager/shared => ../../shared

replace github.com/supabase-manager/version => ../version
