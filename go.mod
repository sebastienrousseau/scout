module github.com/sebastienrousseau/scout

go 1.26.8

require (
	github.com/charmbracelet/bubbles v1.0.0
	github.com/charmbracelet/bubbletea v1.3.10
	github.com/charmbracelet/lipgloss v1.1.0
	github.com/charmbracelet/x/term v0.2.2
	github.com/mattn/go-isatty v0.0.24
	github.com/muesli/termenv v0.16.0
	github.com/sebastienrousseau/scout-reporting v0.0.4
	github.com/spf13/cobra v1.10.2
	github.com/spf13/pflag v1.0.10
)

require (
	github.com/aymanbagabas/go-osc52/v2 v2.0.1 // indirect
	github.com/charmbracelet/colorprofile v0.4.1 // indirect
	github.com/charmbracelet/x/ansi v0.11.6 // indirect
	github.com/charmbracelet/x/cellbuf v0.0.15 // indirect
	github.com/clipperhouse/displaywidth v0.9.0 // indirect
	github.com/clipperhouse/stringish v0.1.1 // indirect
	github.com/clipperhouse/uax29/v2 v2.5.0 // indirect
	github.com/cpuguy83/go-md2man/v2 v2.0.6 // indirect
	github.com/erikgeiser/coninput v0.0.0-20211004153227-1c3628e74d0f // indirect
	github.com/inconshreveable/mousetrap v1.1.0 // indirect
	github.com/lucasb-eyer/go-colorful v1.3.0 // indirect
	github.com/mattn/go-localereader v0.0.1 // indirect
	github.com/mattn/go-runewidth v0.0.19 // indirect
	github.com/muesli/ansi v0.0.0-20230316100256-276c6243b2f6 // indirect
	github.com/muesli/cancelreader v0.2.2 // indirect
	github.com/rivo/uniseg v0.4.7 // indirect
	github.com/russross/blackfriday/v2 v2.1.0 // indirect
	github.com/xo/terminfo v0.0.0-20220910002029-abceb7e1c41e // indirect
	go.yaml.in/yaml/v3 v3.0.4 // indirect
	golang.org/x/sys v0.44.0 // indirect
	golang.org/x/text v0.42.0 // indirect
)

// v0.1.0 was tagged early in the project's life and the tag was deleted,
// but the module proxy never forgets a version: @latest resolves to it
// rather than to the current 0.0.x release, and gorelease compares against
// it. A retraction only takes effect from a version higher than the one it
// retracts, so this directive is inert until a version above v0.1.0 carries
// it; it is recorded here so that release is a one-line tag when decided.
retract [v0.1.0, v0.1.0]
