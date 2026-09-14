package internal

// Version identifies this build. A release overrides it with
// TODO fix the location of main.Version
// -ldflags "-X main.Version=<tag>", and it also identifies the CLI to the meshStack
// API through the client's user agent.
var Version = "dev"
