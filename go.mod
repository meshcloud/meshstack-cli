module github.com/meshcloud/meshstack-cli

go 1.26 // keep flake.nix's pinned Go (go_1_26 + GOROOT) in lock-step when bumping

require (
	github.com/spf13/cobra v1.10.2
	github.com/stretchr/testify v1.12.1
)

require (
	github.com/inconshreveable/mousetrap v1.1.0 // indirect
	github.com/spf13/pflag v1.0.9 // indirect
	go.yaml.in/yaml/v3 v3.0.5 // indirect
)
